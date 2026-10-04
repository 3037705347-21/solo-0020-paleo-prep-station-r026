package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/httpapi"
	"example.com/paleo-prep-station/internal/repository"
	"example.com/paleo-prep-station/internal/station"
)

type specimenView struct {
	ID            string `json:"id"`
	CatalogNumber string `json:"catalog_number"`
	Status        string `json:"status"`
	Version       int    `json:"version"`
}

type planView struct {
	ID                   string `json:"id"`
	SpecimenID           string `json:"specimen_id"`
	Status               string `json:"status"`
	ReevaluationRequired bool   `json:"reevaluation_required"`
	Version              int    `json:"version"`
}

type amendmentView struct {
	ID            string   `json:"id"`
	Sequence      int      `json:"sequence"`
	ChangedFields []string `json:"changed_fields"`
	AffectsPlan   bool     `json:"affects_plan"`
	PlanID        string   `json:"plan_id"`
	BaseVersion   int      `json:"base_version"`
	NewVersion    int      `json:"new_version"`
}

type amendmentResult struct {
	Specimen  specimenView  `json:"specimen"`
	Amendment amendmentView `json:"amendment"`
	Plan      *planView     `json:"plan"`
}

type errorView struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type planSnapshot struct {
	Plan     planView `json:"plan"`
	Progress []struct {
		Percent int `json:"percent"`
	} `json:"progress"`
}

type releaseView struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
}

func main() {
	workflow := flag.String("workflow", "", "workflow id to check")
	flag.Parse()

	if err := run(*workflow); err != nil {
		fmt.Fprintln(os.Stderr, "workflow check failed:", err)
		os.Exit(1)
	}
	fmt.Printf("workflow check passed: %s\n", *workflow)
}

func run(workflow string) error {
	handler := httpapi.NewRouter(station.New(repository.NewMemory()))
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}

	switch workflow {
	case "register-specimen":
		specimen, err := registerSpecimen(client, server.URL)
		if err != nil {
			return err
		}
		if specimen.Status != "registered" {
			return fmt.Errorf("expected registered status, got %s", specimen.Status)
		}
		var fetched specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetched); err != nil {
			return err
		}
		if fetched.ID != specimen.ID {
			return errors.New("registered specimen cannot be read back")
		}
		return nil

	case "approve-prep-plan":
		specimen, err := registerSpecimen(client, server.URL)
		if err != nil {
			return err
		}
		plan, err := createPlan(client, server.URL, specimen.ID)
		if err != nil {
			return err
		}
		var approved planView
		if err := requestJSON(
			client,
			http.MethodPost,
			server.URL+"/plans/"+plan.ID+"/approval",
			contracts.ApprovalRequest{ApprovedBy: "curator-lin"},
			http.StatusOK,
			&approved,
		); err != nil {
			return err
		}
		if approved.Status != "approved" {
			return fmt.Errorf("expected approved plan, got %s", approved.Status)
		}
		var fetchedSpecimen specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetchedSpecimen); err != nil {
			return err
		}
		if fetchedSpecimen.Status != "preparing" {
			return fmt.Errorf("expected preparing specimen, got %s", fetchedSpecimen.Status)
		}
		return nil

	case "log-preparation":
		specimen, plan, err := completedPlanSetup(client, server.URL)
		if err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 35, "矩阵剥离"); err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 100, "表面加固"); err != nil {
			return err
		}
		var snapshot planSnapshot
		if err := requestJSON(client, http.MethodGet, server.URL+"/plans/"+plan.ID, nil, http.StatusOK, &snapshot); err != nil {
			return err
		}
		if snapshot.Plan.Status != "completed" || len(snapshot.Progress) != 2 {
			return errors.New("plan did not complete with exactly two progress entries")
		}
		var fetchedSpecimen specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetchedSpecimen); err != nil {
			return err
		}
		if fetchedSpecimen.Status != "stabilized" {
			return fmt.Errorf("expected stabilized specimen, got %s", fetchedSpecimen.Status)
		}
		return nil

	case "release-for-study":
		specimen, plan, err := completedPlanSetup(client, server.URL)
		if err != nil {
			return err
		}
		if err := recordProgress(client, server.URL, plan.ID, 100, "表面加固"); err != nil {
			return err
		}
		var release releaseView
		if err := requestJSON(
			client,
			http.MethodPost,
			server.URL+"/specimens/"+specimen.ID+"/release",
			contracts.ReleaseRequest{
				Purpose:    "比较解剖研究",
				Recipient:  "地层研究组",
				ApprovedBy: "keeper-zhou",
			},
			http.StatusCreated,
			&release,
		); err != nil {
			return err
		}
		if release.SpecimenID != specimen.ID {
			return errors.New("release record references the wrong specimen")
		}
		var fetched specimenView
		if err := requestJSON(client, http.MethodGet, server.URL+"/specimens/"+specimen.ID, nil, http.StatusOK, &fetched); err != nil {
			return err
		}
		if fetched.Status != "available" {
			return fmt.Errorf("expected available specimen, got %s", fetched.Status)
		}
		return nil

	case "amend-specimen":
		return runAmendSpecimen(client, server.URL)
	default:
		return fmt.Errorf("unknown workflow %q", workflow)
	}
}

func runAmendSpecimen(client *http.Client, baseURL string) error {
	specimen, err := registerSpecimen(client, baseURL)
	if err != nil {
		return err
	}
	base := baseURL + "/specimens/" + specimen.ID

	// 1. Validation: no field supplied.
	if code, err := expectErrorCode(client, http.MethodPatch, base,
		contracts.AmendSpecimenRequest{Reason: "登记核对", ExpectedVersion: 1},
		http.StatusBadRequest, "no_amendable_field"); err != nil {
		return fmt.Errorf("no-field amendment: code=%s %w", code, err)
	}

	// 2. Validation: missing reason.
	taxon := "大型蜥脚类"
	if code, err := expectErrorCode(client, http.MethodPatch, base,
		contracts.AmendSpecimenRequest{TaxonHint: &taxon, ExpectedVersion: 1},
		http.StatusBadRequest, "required_field"); err != nil {
		return fmt.Errorf("no-reason amendment: code=%s %w", code, err)
	}

	// 3. Validation: stale/missing expected_version.
	if code, err := expectErrorCode(client, http.MethodPatch, base,
		contracts.AmendSpecimenRequest{TaxonHint: &taxon, Reason: "登记核对", ExpectedVersion: 0},
		http.StatusBadRequest, "invalid_expected_version"); err != nil {
		return fmt.Errorf("missing expected_version: code=%s %w", code, err)
	}

	// 4. Successful reason-bearing amendment bumps version and retains history.
	var first amendmentResult
	if err := requestJSON(client, http.MethodPatch, base,
		contracts.AmendSpecimenRequest{
			TaxonHint:       &taxon,
			Reason:          "野外台账核对后修正分类提示",
			ExpectedVersion: 1,
			RequestID:       "amend-2026-0001",
		},
		http.StatusOK, &first); err != nil {
		return err
	}
	if first.Specimen.Version != 2 || first.Amendment.NewVersion != 2 ||
		first.Amendment.BaseVersion != 1 || first.Amendment.Sequence != 1 ||
		len(first.Amendment.ChangedFields) != 1 ||
		first.Amendment.ChangedFields[0] != "taxon_hint" {
		return fmt.Errorf("first amendment result unexpected: %+v", first)
	}

	// 5. Repeated submission: identical request id replays the exact result.
	var replay amendmentResult
	if err := requestJSON(client, http.MethodPatch, base,
		contracts.AmendSpecimenRequest{
			TaxonHint:       &taxon,
			Reason:          "野外台账核对后修正分类提示",
			ExpectedVersion: 1,
			RequestID:       "amend-2026-0001",
		},
		http.StatusOK, &replay); err != nil {
		return err
	}
	if replay.Amendment.ID != first.Amendment.ID || replay.Specimen.Version != 2 {
		return errors.New("idempotent replay did not return the original amendment")
	}

	// 6. Reused request id with a different payload is a hard conflict.
	otherMatrix := "石灰岩"
	if code, err := expectErrorCode(client, http.MethodPatch, base,
		contracts.AmendSpecimenRequest{
			MatrixKind:      &otherMatrix,
			Reason:          "野外台账核对后修正基质",
			ExpectedVersion: 2,
			RequestID:       "amend-2026-0001",
		},
		http.StatusConflict, "idempotency_key_reused"); err != nil {
		return fmt.Errorf("reused request id: code=%s %w", code, err)
	}

	// 7. No-change submission (same values, new request id) is deterministic.
	if code, err := expectErrorCode(client, http.MethodPatch, base,
		contracts.AmendSpecimenRequest{
			TaxonHint:       &taxon,
			Reason:          "再次核对",
			ExpectedVersion: 2,
			RequestID:       "amend-2026-0002",
		},
		http.StatusConflict, "specimen_unchanged"); err != nil {
		return fmt.Errorf("unchanged amendment: code=%s %w", code, err)
	}

	// 8. Concurrent modification: expected_version 1 now loses to version 2.
	jacket := "夹克边角加固条已脱落"
	if code, err := expectErrorCode(client, http.MethodPatch, base,
		contracts.AmendSpecimenRequest{
			JacketNotes:     &jacket,
			Reason:          "基于旧版本并发修改",
			ExpectedVersion: 1,
			RequestID:       "amend-2026-0003",
		},
		http.StatusConflict, "specimen_version_conflict"); err != nil {
		return fmt.Errorf("concurrent version conflict: code=%s %w", code, err)
	}

	// 9. Jacket-only correction with no plan does not touch any plan.
	var jacketAmendment amendmentResult
	if err := requestJSON(client, http.MethodPatch, base,
		contracts.AmendSpecimenRequest{
			JacketNotes:     &jacket,
			Reason:          "运输复查修正夹克备注",
			ExpectedVersion: 2,
			RequestID:       "amend-2026-0004",
		},
		http.StatusOK, &jacketAmendment); err != nil {
		return err
	}
	if jacketAmendment.Specimen.Version != 3 || jacketAmendment.Plan != nil ||
		jacketAmendment.Amendment.AffectsPlan {
		return fmt.Errorf("jacket-only amendment should not affect a plan: %+v", jacketAmendment)
	}

	// 10. Draft plan + method-affecting correction forces reevaluation.
	plan, err := createPlan(client, baseURL, specimen.ID)
	if err != nil {
		return err
	}
	// Specimen advanced to planned (version 4) when the plan was created.
	matrix := "泥岩"
	var flagged amendmentResult
	if err := requestJSON(client, http.MethodPatch, base,
		contracts.AmendSpecimenRequest{
			MatrixKind:      &matrix,
			Reason:          "岩性薄片复核，基质由粉砂岩改为泥岩",
			ExpectedVersion: 4,
			RequestID:       "amend-2026-0005",
		},
		http.StatusOK, &flagged); err != nil {
		return err
	}
	if flagged.Plan == nil || !flagged.Plan.ReevaluationRequired ||
		!flagged.Amendment.AffectsPlan || flagged.Amendment.PlanID != plan.ID {
		return fmt.Errorf("draft plan was not flagged for reevaluation: %+v", flagged)
	}

	// 11. Approval is blocked until reevaluation is resolved.
	if code, err := expectErrorCode(client, http.MethodPost,
		baseURL+"/plans/"+plan.ID+"/approval",
		contracts.ApprovalRequest{ApprovedBy: "curator-lin"},
		http.StatusConflict, "plan_reevaluation_pending"); err != nil {
		return fmt.Errorf("approval while pending: code=%s %w", code, err)
	}

	// 12. Resolve reevaluation, then approval succeeds.
	var reviewed planView
	if err := requestJSON(client, http.MethodPost,
		baseURL+"/plans/"+plan.ID+"/reevaluation",
		contracts.ReevaluationRequest{
			ReviewedBy: "lead-wang",
			Note:       "手工显微剥离对泥岩仍然适用，维持原目标",
		},
		http.StatusOK, &reviewed); err != nil {
		return err
	}
	if reviewed.ReevaluationRequired {
		return errors.New("reevaluation flag was not cleared")
	}
	var approved planView
	if err := requestJSON(client, http.MethodPost,
		baseURL+"/plans/"+plan.ID+"/approval",
		contracts.ApprovalRequest{ApprovedBy: "curator-lin"},
		http.StatusOK, &approved); err != nil {
		return err
	}
	if approved.Status != "approved" {
		return fmt.Errorf("expected approved plan, got %s", approved.Status)
	}

	// 13. Reference conflict: approved plan blocks method-affecting correction.
	matrix2 := "钙质砂岩"
	if code, err := expectErrorCode(client, http.MethodPatch, base,
		contracts.AmendSpecimenRequest{
			MatrixKind:      &matrix2,
			Reason:          "审批后再次发现基质错误",
			ExpectedVersion: 6,
			RequestID:       "amend-2026-0006",
		},
		http.StatusConflict, "plan_references_specimen_data"); err != nil {
		return fmt.Errorf("approved-plan correction: code=%s %w", code, err)
	}
	// Jacket notes remain correctable even while preparation is open.
	jacket2 := "夹克已拆除"
	var jacketDuringPrep amendmentResult
	if err := requestJSON(client, http.MethodPatch, base,
		contracts.AmendSpecimenRequest{
			JacketNotes:     &jacket2,
			Reason:          "夹克在制备中拆除",
			ExpectedVersion: 6,
			RequestID:       "amend-2026-0007",
		},
		http.StatusOK, &jacketDuringPrep); err != nil {
		return err
	}
	if jacketDuringPrep.Plan != nil {
		return errors.New("jacket correction during preparation must not flag the plan")
	}

	// 14. Stabilized specimen cannot be amended at all.
	if err := recordProgress(client, baseURL, plan.ID, 100, "表面加固"); err != nil {
		return err
	}
	jacket3 := "禁止改写"
	if code, err := expectErrorCode(client, http.MethodPatch, base,
		contracts.AmendSpecimenRequest{
			JacketNotes:     &jacket3,
			Reason:          "稳定后试图改写",
			ExpectedVersion: 8,
			RequestID:       "amend-2026-0008",
		},
		http.StatusConflict, "specimen_not_amendable"); err != nil {
		return fmt.Errorf("stabilized amendment: code=%s %w", code, err)
	}

	// 15. Amendment history is retained in order.
	var history struct {
		Items []amendmentView `json:"items"`
	}
	if err := requestJSON(client, http.MethodGet, base+"/amendments", nil,
		http.StatusOK, &history); err != nil {
		return err
	}
	if len(history.Items) != 4 {
		return fmt.Errorf("expected 4 retained amendments, got %d", len(history.Items))
	}
	for index, item := range history.Items {
		if item.Sequence != index+1 {
			return fmt.Errorf("amendment history out of order at %d: %+v", index, item)
		}
	}
	return nil
}

func registerSpecimen(client *http.Client, baseURL string) (specimenView, error) {
	var specimen specimenView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens",
		contracts.RegisterSpecimenRequest{
			CatalogNumber: "PPS-2026-001",
			FieldLabel:    "北坡-07",
			TaxonHint:     "大型脊椎动物",
			MatrixKind:    "粉砂岩",
			JacketNotes:   "夹克边角完好",
		},
		http.StatusCreated,
		&specimen,
	)
	return specimen, err
}

func createPlan(client *http.Client, baseURL, specimenID string) (planView, error) {
	var plan planView
	err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimenID+"/plans",
		contracts.CreatePlanRequest{
			Objective:      "暴露主要关节面",
			Method:         "手工显微剥离",
			Adhesive:       "可逆树脂",
			TargetExposure: 80,
		},
		http.StatusCreated,
		&plan,
	)
	return plan, err
}

func completedPlanSetup(client *http.Client, baseURL string) (specimenView, planView, error) {
	specimen, err := registerSpecimen(client, baseURL)
	if err != nil {
		return specimenView{}, planView{}, err
	}
	plan, err := createPlan(client, baseURL, specimen.ID)
	if err != nil {
		return specimenView{}, planView{}, err
	}
	var approved planView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+plan.ID+"/approval",
		contracts.ApprovalRequest{ApprovedBy: "curator-lin"},
		http.StatusOK,
		&approved,
	); err != nil {
		return specimenView{}, planView{}, err
	}
	return specimen, approved, nil
}

func recordProgress(client *http.Client, baseURL, planID string, percent int, stage string) error {
	var ignored struct{}
	return requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+planID+"/progress",
		contracts.ProgressRequest{
			Stage:      stage,
			Percent:    percent,
			Note:       "本地链路检查",
			RecordedBy: "preparator-chen",
		},
		http.StatusCreated,
		&ignored,
	)
}

func requestJSON(
	client *http.Client,
	method string,
	url string,
	body any,
	wantStatus int,
	target any,
) error {
	code, err := doJSON(client, method, url, body, wantStatus, target)
	if err != nil {
		return err
	}
	if code != "" {
		return fmt.Errorf("request %s %s failed with code %s", method, url, code)
	}
	return nil
}

// expectErrorCode performs a request expected to fail and asserts both the
// HTTP status and the stable machine-readable error code.
func expectErrorCode(
	client *http.Client,
	method string,
	url string,
	body any,
	wantStatus int,
	wantCode string,
) (string, error) {
	var failure errorView
	code, err := doJSON(client, method, url, body, wantStatus, &failure)
	if err != nil {
		return code, err
	}
	if code == "" {
		return "", fmt.Errorf("request %s %s unexpectedly succeeded", method, url)
	}
	if code != wantCode {
		return code, fmt.Errorf("request %s %s returned error code %s, want %s", method, url, code, wantCode)
	}
	return code, nil
}

func doJSON(
	client *http.Client,
	method string,
	url string,
	body any,
	wantStatus int,
	target any,
) (string, error) {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return "", err
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, payload)
	if err != nil {
		return "", err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode != wantStatus {
		return "", fmt.Errorf("request %s %s returned %d: %s", method, url, response.StatusCode, string(raw))
	}
	if response.StatusCode >= 400 {
		var failure errorView
		if err := json.Unmarshal(raw, &failure); err != nil {
			return "", fmt.Errorf("decode error response for %s %s: %w", method, url, err)
		}
		return failure.Code, nil
	}
	if target == nil || len(raw) == 0 {
		return "", nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return "", fmt.Errorf("decode response for %s %s: %w", method, url, err)
	}
	return "", nil
}
