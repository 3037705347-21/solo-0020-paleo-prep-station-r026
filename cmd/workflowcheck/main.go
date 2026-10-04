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
}

type planView struct {
	ID         string `json:"id"`
	SpecimenID string `json:"specimen_id"`
	Status     string `json:"status"`
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

type specimenFullView struct {
	ID            string `json:"id"`
	CatalogNumber string `json:"catalog_number"`
	Status        string `json:"status"`
	Version       int    `json:"version"`
	TaxonHint     string `json:"taxon_hint"`
	MatrixKind    string `json:"matrix_kind"`
	JacketNotes   string `json:"jacket_notes"`
}

type amendResultView struct {
	Specimen             specimenFullView `json:"specimen"`
	PlanImpact           bool             `json:"plan_impact"`
	PlanReset            bool             `json:"plan_reset"`
	ReevaluationRequired bool             `json:"reevaluation_required"`
	Plan                 *planFullView    `json:"plan"`
	Revision             struct {
		Sequence      int      `json:"sequence"`
		FromVersion   int      `json:"from_version"`
		ToVersion     int      `json:"to_version"`
		ChangedFields []string `json:"changed_fields"`
		Reason        string   `json:"reason"`
	} `json:"revision"`
}

type planFullView struct {
	ID                   string `json:"id"`
	SpecimenID           string `json:"specimen_id"`
	Status               string `json:"status"`
	Version              int    `json:"version"`
	ReevaluationRequired bool   `json:"reevaluation_required"`
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
		return checkAmendSpecimen(client, server.URL)

	case "amend-plan-reevaluation":
		return checkAmendPlanReevaluation(client, server.URL)

	case "amend-lock-and-conflict":
		return checkAmendLockAndConflict(client, server.URL)

	default:
		return fmt.Errorf("unknown workflow %q", workflow)
	}
}

// checkAmendSpecimen covers the no-plan correction path: version bump,
// reason-required, no-change, duplicate submit and optimistic concurrency.
func checkAmendSpecimen(client *http.Client, baseURL string) error {
	specimen, err := registerSpecimen(client, baseURL)
	if err != nil {
		return err
	}

	var result amendResultView
	if err := requestJSON(
		client,
		http.MethodPatch,
		baseURL+"/specimens/"+specimen.ID,
		map[string]any{
			"taxon_hint":       "小型兽脚类",
			"jacket_notes":     "夹克边角需重新加固",
			"reason":           "野外标签复核后更正分类与夹克状况",
			"expected_version": 1,
		},
		http.StatusOK,
		&result,
	); err != nil {
		return err
	}
	if result.Specimen.Version != 2 ||
		result.Specimen.TaxonHint != "小型兽脚类" ||
		result.Revision.ToVersion != 2 ||
		result.Revision.Sequence != 1 {
		return errors.New("amendment did not produce version 2 with a first revision")
	}

	// Duplicate submit of the exact same body: stale version is a conflict.
	if err := expectStatus(
		client,
		http.MethodPatch,
		baseURL+"/specimens/"+specimen.ID,
		map[string]any{
			"taxon_hint":       "小型兽脚类",
			"jacket_notes":     "夹克边角需重新加固",
			"reason":           "重复提交",
			"expected_version": 1,
		},
		http.StatusConflict,
	); err != nil {
		return err
	}

	// Same values on the current version: nothing changed is a client error.
	if err := expectStatus(
		client,
		http.MethodPatch,
		baseURL+"/specimens/"+specimen.ID,
		map[string]any{
			"taxon_hint":       "小型兽脚类",
			"reason":           "无实际变化",
			"expected_version": 2,
		},
		http.StatusConflict,
	); err != nil {
		return err
	}

	// Missing reason is invalid input.
	if err := expectStatus(
		client,
		http.MethodPatch,
		baseURL+"/specimens/"+specimen.ID,
		map[string]any{
			"matrix_kind":      "泥岩",
			"expected_version": 2,
		},
		http.StatusBadRequest,
	); err != nil {
		return err
	}

	// Concurrent edit: stale base version loses even with new values.
	if err := expectStatus(
		client,
		http.MethodPatch,
		baseURL+"/specimens/"+specimen.ID,
		map[string]any{
			"matrix_kind":      "泥岩",
			"reason":           "并发的另一笔修改",
			"expected_version": 1,
		},
		http.StatusConflict,
	); err != nil {
		return err
	}

	// Fresh base version succeeds and keeps revision history ordered.
	if err := requestJSON(
		client,
		http.MethodPatch,
		baseURL+"/specimens/"+specimen.ID,
		map[string]any{
			"matrix_kind":      "泥岩",
			"reason":           "镜下复核基质为泥岩",
			"expected_version": 2,
		},
		http.StatusOK,
		&result,
	); err != nil {
		return err
	}
	if result.Specimen.Version != 3 || result.Specimen.MatrixKind != "泥岩" {
		return errors.New("second amendment did not produce version 3")
	}

	var revisions struct {
		Items []struct {
			Sequence int `json:"sequence"`
		} `json:"items"`
	}
	if err := requestJSON(
		client,
		http.MethodGet,
		baseURL+"/specimens/"+specimen.ID+"/revisions",
		nil,
		http.StatusOK,
		&revisions,
	); err != nil {
		return err
	}
	if len(revisions.Items) != 2 ||
		revisions.Items[0].Sequence != 1 ||
		revisions.Items[1].Sequence != 2 {
		return errors.New("revision history is missing or out of order")
	}
	return nil
}

// checkAmendPlanReevaluation covers how corrections interact with a plan:
// draft plan flagged for review, approval blocked, re-evaluation confirmed;
// then an approved plan is forced back to draft by an impacting change.
func checkAmendPlanReevaluation(client *http.Client, baseURL string) error {
	specimen, err := registerSpecimen(client, baseURL)
	if err != nil {
		return err
	}
	plan, err := createPlan(client, baseURL, specimen.ID)
	if err != nil {
		return err
	}

	// Impacting change while the plan is still a draft: flag, do not block.
	// Creating the plan advanced the specimen to planned version 2.
	var draftAmend amendResultView
	if err := requestJSON(
		client,
		http.MethodPatch,
		baseURL+"/specimens/"+specimen.ID,
		map[string]any{
			"matrix_kind":      "泥灰岩",
			"reason":           "基质判读更正",
			"expected_version": 2,
		},
		http.StatusOK,
		&draftAmend,
	); err != nil {
		return err
	}
	if !draftAmend.ReevaluationRequired ||
		draftAmend.Plan == nil ||
		!draftAmend.Plan.ReevaluationRequired ||
		draftAmend.Plan.Status != "draft" {
		return errors.New("draft plan was not marked for re-evaluation")
	}

	// Approval must be refused until re-evaluation is confirmed.
	if err := expectStatus(
		client,
		http.MethodPost,
		baseURL+"/plans/"+plan.ID+"/approval",
		contracts.ApprovalRequest{ApprovedBy: "curator-lin"},
		http.StatusConflict,
	); err != nil {
		return err
	}

	var reviewed planFullView
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+plan.ID+"/reevaluation",
		contracts.ReevaluationRequest{ConfirmedBy: "curator-lin"},
		http.StatusOK,
		&reviewed,
	); err != nil {
		return err
	}
	if reviewed.ReevaluationRequired || reviewed.Status != "draft" {
		return errors.New("re-evaluation confirmation did not clear the flag")
	}

	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/plans/"+plan.ID+"/approval",
		contracts.ApprovalRequest{ApprovedBy: "curator-lin"},
		http.StatusOK,
		&reviewed,
	); err != nil {
		return err
	}

	// Jacket notes do not affect method/objective: allowed while preparing.
	// The first amendment bumped to v3 and approval advanced to v4.
	if err := expectStatus(
		client,
		http.MethodPatch,
		baseURL+"/specimens/"+specimen.ID,
		map[string]any{
			"jacket_notes":     "补充运输损伤备注",
			"reason":           "入库检查补充",
			"expected_version": 4,
		},
		http.StatusOK,
	); err != nil {
		return err
	}

	// Impacting change on an approved plan is blocked without force.
	if err := expectStatus(
		client,
		http.MethodPatch,
		baseURL+"/specimens/"+specimen.ID,
		map[string]any{
			"matrix_kind":      "钙质结核",
			"reason":           "再次判读基质",
			"expected_version": 5,
		},
		http.StatusConflict,
	); err != nil {
		return err
	}

	// Forcing re-evaluation returns the approved plan to draft atomically.
	var forced amendResultView
	if err := requestJSON(
		client,
		http.MethodPatch,
		baseURL+"/specimens/"+specimen.ID,
		map[string]any{
			"matrix_kind":        "钙质结核",
			"reason":             "再次判读基质，需重评方法",
			"expected_version":   5,
			"force_reevaluation": true,
		},
		http.StatusOK,
		&forced,
	); err != nil {
		return err
	}
	if !forced.PlanReset ||
		forced.Plan == nil ||
		forced.Plan.Status != "draft" ||
		!forced.Plan.ReevaluationRequired {
		return errors.New("approved plan was not returned to draft for re-evaluation")
	}
	return nil
}

// checkAmendLockAndConflict covers terminal locks and reference integrity:
// active plans block impacting edits, stabilized/released specimens are
// frozen, and unknown specimens return not-found.
func checkAmendLockAndConflict(client *http.Client, baseURL string) error {
	specimen, plan, err := completedPlanSetup(client, baseURL)
	if err != nil {
		return err
	}
	// First progress entry moves the plan to active; specimen is at v3.
	if err := recordProgress(client, baseURL, plan.ID, 40, "基质剥离"); err != nil {
		return err
	}

	if err := expectStatus(
		client,
		http.MethodPatch,
		baseURL+"/specimens/"+specimen.ID,
		map[string]any{
			"taxon_hint":       "疑误定种",
			"reason":           "制备中发现新特征",
			"expected_version": 3,
		},
		http.StatusConflict,
	); err != nil {
		return err
	}
	// Non-impacting jacket notes stay editable while work is in progress.
	if err := expectStatus(
		client,
		http.MethodPatch,
		baseURL+"/specimens/"+specimen.ID,
		map[string]any{
			"jacket_notes":     "制备中新记录",
			"reason":           "制备中补充",
			"expected_version": 3,
		},
		http.StatusOK,
	); err != nil {
		return err
	}

	// Complete preparation: specimen stabilizes (v5) and amendments freeze.
	if err := recordProgress(client, baseURL, plan.ID, 100, "表面加固"); err != nil {
		return err
	}
	if err := expectStatus(
		client,
		http.MethodPatch,
		baseURL+"/specimens/"+specimen.ID,
		map[string]any{
			"jacket_notes":     "稳定后也不允许改",
			"reason":           "尝试改写",
			"expected_version": 5,
		},
		http.StatusConflict,
	); err != nil {
		return err
	}

	// Release, then confirm the lock persists on the terminal specimen.
	if err := requestJSON(
		client,
		http.MethodPost,
		baseURL+"/specimens/"+specimen.ID+"/release",
		contracts.ReleaseRequest{
			Purpose:    "比较解剖研究",
			Recipient:  "地层研究组",
			ApprovedBy: "keeper-zhou",
		},
		http.StatusCreated,
		&releaseView{},
	); err != nil {
		return err
	}
	// Release advanced the specimen to available version 6.
	if err := expectStatus(
		client,
		http.MethodPatch,
		baseURL+"/specimens/"+specimen.ID,
		map[string]any{
			"matrix_kind":      "粉砂岩",
			"reason":           "放行后尝试改写",
			"expected_version": 6,
		},
		http.StatusConflict,
	); err != nil {
		return err
	}

	// Reference integrity: unknown specimen is a stable not-found result.
	if err := expectStatus(
		client,
		http.MethodPatch,
		baseURL+"/specimens/specimen-does-not-exist",
		map[string]any{
			"reason":           "不存在的标本",
			"expected_version": 1,
		},
		http.StatusNotFound,
	); err != nil {
		return err
	}
	return nil
}

// expectStatus issues a JSON request and only asserts the response status.
func expectStatus(
	client *http.Client,
	method string,
	url string,
	body any,
	wantStatus int,
) error {
	return requestJSON(client, method, url, body, wantStatus, nil)
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
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, payload)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != wantStatus {
		return fmt.Errorf("request %s %s returned %d: %s", method, url, response.StatusCode, string(raw))
	}
	if target == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode response for %s %s: %w", method, url, err)
	}
	return nil
}
