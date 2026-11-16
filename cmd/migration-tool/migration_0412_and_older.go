package main

import (
	"io"
	"os"
	"strings"

	interfaces "github.com/ReCasaOS/CasaOS-Common"

	"github.com/ReCasaOS/CasaOS-AppManagement/pkg/config"
	"github.com/ReCasaOS/CasaOS-AppManagement/service"
)

type migrationTool0412AndOlder struct{}

const bigBearAppStoreUrl = "https://github.com/bigbeartechworld/big-bear-casaos/archive/refs/heads/master.zip"

func (u *migrationTool0412AndOlder) IsMigrationNeeded() (bool, error) {
	_logger.Info("Checking if migration is needed...")

	file, err := os.Open(config.AppManagementConfigFilePath)
	if err != nil {
		_logger.Error("failed to detect app management config file: %s", err)
		return false, err
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		_logger.Error("failed to read app management config file: %s", err)
		return false, err
	}

	if strings.Contains(string(content), bigBearAppStoreUrl) {
		_logger.Info("Migration will replace dead Big Bear app store URL.")
		return true, nil
	}
	_logger.Info("Big Bear app store migration skipped; URL not present.")
	return false, nil
}

func (u *migrationTool0412AndOlder) PreMigrate() error {
	return nil
}

func (u *migrationTool0412AndOlder) Migrate() error {
	file, err := os.OpenFile(config.AppManagementConfigFilePath, os.O_RDWR, 0644)
	if err != nil {
		_logger.Error("failed to open app management config file: %s", err)
		return err
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		_logger.Error("failed to read app management config file: %s", err)
		return err
	}

	if !strings.Contains(string(content), bigBearAppStoreUrl) {
		return nil
	}

	successor, dead := service.SuccessorOf(bigBearAppStoreUrl)
	if !dead {
		return nil
	}

	newContent := strings.Replace(string(content), bigBearAppStoreUrl, successor, -1)

	if err = file.Truncate(0); err != nil {
		_logger.Error("failed to truncate app management config file: %s", err)
		return err
	}

	if _, err = file.WriteAt([]byte(newContent), 0); err != nil {
		_logger.Error("failed to write app management config file: %s", err)
		return err
	}

	return nil
}

func (u *migrationTool0412AndOlder) PostMigrate() error {
	return nil
}

func NewMigration0412AndOlder() interfaces.MigrationTool {
	return &migrationTool0412AndOlder{}
}