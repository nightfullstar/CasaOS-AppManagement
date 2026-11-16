package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ReCasaOS/CasaOS-AppManagement/pkg/config"
)

func TestMain(m *testing.M) {
	_logger = NewLogger()
	os.Exit(m.Run())
}

func withTempAppManagementConf(t *testing.T, body string) func() {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "app-management.conf")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	prev := config.AppManagementConfigFilePath
	config.AppManagementConfigFilePath = path
	return func() { config.AppManagementConfigFilePath = prev }
}

func TestMigration0412_IsMigrationNeeded_falseWithoutBigBear(t *testing.T) {
	defer withTempAppManagementConf(t, "[server]\nappstore = https://recasaos.org/store/recasaos.zip\n")()

	m := &migrationTool0412AndOlder{}
	needed, err := m.IsMigrationNeeded()
	if err != nil {
		t.Fatal(err)
	}
	if needed {
		t.Fatal("expected no migration when Big Bear URL is absent")
	}
}

func TestMigration0412_Migrate_doesNotAppendBigBear(t *testing.T) {
	const before = "[server]\nappstore = https://recasaos.org/store/recasaos.zip\n"
	defer withTempAppManagementConf(t, before)()

	m := &migrationTool0412AndOlder{}
	if err := m.Migrate(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(config.AppManagementConfigFilePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != before {
		t.Fatalf("conf changed unexpectedly:\n%s", after)
	}
	if strings.Contains(string(after), bigBearAppStoreUrl) {
		t.Fatal("must not append the dead Big Bear store URL")
	}
}

func TestMigration0412_IsMigrationNeeded_trueWithDeadURL(t *testing.T) {
	conf := "[server]\nappstore = " + bigBearAppStoreUrl + "\n"
	defer withTempAppManagementConf(t, conf)()

	m := &migrationTool0412AndOlder{}
	needed, err := m.IsMigrationNeeded()
	if err != nil {
		t.Fatal(err)
	}
	if !needed {
		t.Fatal("expected migration when dead Big Bear URL is present")
	}
}

func TestMigration0412_Migrate_replacesDeadURLWithOrgCatalogue(t *testing.T) {
	const org = "https://recasaos.org/store/recasaos.zip"
	conf := "[server]\nappstore = " + bigBearAppStoreUrl + "\nappstore = https://example.com/other.zip\n"
	defer withTempAppManagementConf(t, conf)()

	m := &migrationTool0412AndOlder{}
	if err := m.Migrate(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(config.AppManagementConfigFilePath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(after)
	if strings.Contains(got, bigBearAppStoreUrl) {
		t.Fatal("dead URL must be removed")
	}
	if !strings.Contains(got, "appstore = "+org) {
		t.Fatalf("expected org catalogue in conf, got:\n%s", got)
	}
	if !strings.Contains(got, "appstore = https://example.com/other.zip") {
		t.Fatal("other appstore lines must be unchanged")
	}
}