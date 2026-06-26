package service

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	opsdoctor "github.com/MalenkiySolovey/solovey-ui/internal/ops/doctor"
)

func initDoctorTestDB(t *testing.T) {
	t.Helper()
	initSettingTestDB(t)
}

func TestDoctorRunReportsMalformedConfig(t *testing.T) {
	initDoctorTestDB(t)
	stored := model.Setting{Key: "config"}
	if err := dbsqlite.DB().
		Where("key = ?", stored.Key).
		Assign(model.Setting{Value: "{bad json"}).
		FirstOrCreate(&stored).Error; err != nil {
		t.Fatalf("corrupt stored config: %v", err)
	}

	report := (&DoctorService{}).Run("example.com")
	if report.Status != opsdoctor.SeverityError {
		t.Fatalf("status = %s, want error: %#v", report.Status, report.Items)
	}
}

func TestDoctorRunReportsMissingReferences(t *testing.T) {
	initDoctorTestDB(t)
	config := `{"log":{"disabled":true},"dns":{"servers":[],"final":"missing-dns","rules":[]},"route":{"final":"missing-out","rules":[{"outbound":"missing-rule"}],"rule_set":[]}}`
	if err := (&SettingService{}).SetConfig(config); err != nil {
		t.Fatalf("set config: %v", err)
	}

	report := (&DoctorService{}).Run("example.com")
	if !doctorReportHas(report, "dns-references", opsdoctor.SeverityError) {
		t.Fatalf("missing dns reference error: %#v", report.Items)
	}
	if !doctorReportHas(report, "route-references", opsdoctor.SeverityError) {
		t.Fatalf("missing route reference error: %#v", report.Items)
	}
}

func TestDiagnoseClientReportsDisabledExpiredAndOverLimit(t *testing.T) {
	initDoctorTestDB(t)
	inbounds, _ := json.Marshal([]uint{})
	client := model.Client{
		Enable:   false,
		Name:     "alice",
		Inbounds: inbounds,
		Volume:   10,
		Up:       10,
		Down:     1,
		Expiry:   1,
	}
	if err := dbsqlite.DB().Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	report, err := (&DoctorService{}).DiagnoseClient(DoctorClientRequest{ClientID: client.Id}, "example.com")
	if err != nil {
		t.Fatalf("DiagnoseClient: %v", err)
	}
	for _, id := range []string{"client-enabled", "client-expiry", "client-traffic", "client-inbounds"} {
		if !doctorReportHas(report, id, opsdoctor.SeverityError) {
			t.Fatalf("missing %s error: %#v", id, report.Items)
		}
	}
}

func TestDiagnoseClientTrafficBoundary(t *testing.T) {
	initDoctorTestDB(t)
	inbounds, _ := json.Marshal([]uint{})

	// used == Volume must count as over-limit (error), and the enabled/non-expired
	// branches must report OK — pinning the OK direction the earlier test never asserts.
	atLimit := model.Client{Enable: true, Name: "atlimit", Inbounds: inbounds, Volume: 10, Up: 6, Down: 4}
	if err := dbsqlite.DB().Create(&atLimit).Error; err != nil {
		t.Fatalf("create atlimit client: %v", err)
	}
	report, err := (&DoctorService{}).DiagnoseClient(DoctorClientRequest{ClientID: atLimit.Id}, "example.com")
	if err != nil {
		t.Fatalf("DiagnoseClient atlimit: %v", err)
	}
	if !doctorReportHas(report, "client-traffic", opsdoctor.SeverityError) {
		t.Fatalf("used==Volume must be over-limit error: %#v", report.Items)
	}
	if !doctorReportHas(report, "client-enabled", opsdoctor.SeverityOK) {
		t.Fatalf("enabled client must report client-enabled OK: %#v", report.Items)
	}
	if !doctorReportHas(report, "client-expiry", opsdoctor.SeverityOK) {
		t.Fatalf("non-expired client must report client-expiry OK: %#v", report.Items)
	}

	// used == Volume-1 must count as within limit (OK).
	under := model.Client{Enable: true, Name: "under", Inbounds: inbounds, Volume: 10, Up: 5, Down: 4}
	if err := dbsqlite.DB().Create(&under).Error; err != nil {
		t.Fatalf("create under client: %v", err)
	}
	report2, err := (&DoctorService{}).DiagnoseClient(DoctorClientRequest{ClientID: under.Id}, "example.com")
	if err != nil {
		t.Fatalf("DiagnoseClient under: %v", err)
	}
	if !doctorReportHas(report2, "client-traffic", opsdoctor.SeverityOK) {
		t.Fatalf("used==Volume-1 must be within limit (OK): %#v", report2.Items)
	}
}

func doctorReportHas(report opsdoctor.Report, id string, severity opsdoctor.Severity) bool {
	for _, item := range report.Items {
		if item.ID == id && item.Severity == severity {
			return true
		}
	}
	return false
}
