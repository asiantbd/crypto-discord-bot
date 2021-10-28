package main

import (
	"log"
	"testing"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

func setViperTest() error {
	viper.SetConfigName("config.test")
	viper.SetConfigType("json")
	viper.AddConfigPath(".")

	return viper.ReadInConfig()
}

func coreGenerator() *Core {
	err := setViperTest()
	if err != nil {
		log.Fatal("failed to set viper variable: ", err)
	}

	cfg, err := generateConfig()
	if err != nil {
		log.Fatal("failed to generate config: ", err)
	}

	core, err := NewCore(cfg, zap.NewNop().Sugar())
	if err != nil {
		log.Fatal("failed to generate config: ", err)
	}

	return core
}

func TestCore_UpdateGasTicker(t *testing.T) {
	core := coreGenerator()
	core.IDMapper()

	tests := []struct {
		name    string
		w       *Core
		wantErr bool
	}{
		{
			name:    "UpdateGasTicker test",
			w:       core,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.w.UpdateGasTicker(); (err != nil) != tt.wantErr {
				t.Errorf("Core.UpdateGasTicker() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCore_UpdatePriceTicker(t *testing.T) {
	core := coreGenerator()
	core.IDMapper()

	tests := []struct {
		name    string
		w       *Core
		wantErr bool
	}{
		{
			name:    "UpdatePriceTicker test",
			w:       core,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.w.UpdatePriceTicker(); (err != nil) != tt.wantErr {
				t.Errorf("Core.UpdatePriceTicker() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCore_IDMapper(t *testing.T) {
	core := coreGenerator()

	tests := []struct {
		name    string
		w       *Core
		wantErr bool
	}{
		{
			name:    "UpdatePriceTicker test",
			w:       core,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.w.IDMapper(); (err != nil) != tt.wantErr {
				t.Errorf("Core.IDMapper() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
