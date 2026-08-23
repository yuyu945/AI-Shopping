package main

import (
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/conf"
)

func TestOrderServiceConfigBuildsPaymentRecoveryWorkerConfig(t *testing.T) {
	var serviceConfig orderServiceConfig
	if err := conf.Load("../etc/order-service.yaml", &serviceConfig); err != nil {
		t.Fatal(err)
	}

	got := serviceConfig.paymentRecoveryWorkerConfig()
	if got.BatchSize != 20 || got.LeaseDuration != 3*time.Minute || got.CallTimeout != 2*time.Second {
		t.Fatalf("paymentRecoveryWorkerConfig() = %#v", got)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("paymentRecoveryWorkerConfig().Validate() error = %v", err)
	}
	if serviceConfig.PaymentRecovery.PollInterval != time.Second {
		t.Fatalf("PaymentRecovery.PollInterval = %s, want 1s", serviceConfig.PaymentRecovery.PollInterval)
	}
}

func TestOrderServicePaymentRecoveryConfigRejectsLeaseBelowBatchBudget(t *testing.T) {
	config := orderServiceConfig{PaymentRecovery: paymentRecoveryConfig{
		BatchSize:     20,
		LeaseDuration: 30 * time.Second,
		CallTimeout:   2 * time.Second,
	}}

	if err := config.paymentRecoveryWorkerConfig().Validate(); err == nil {
		t.Fatal("paymentRecoveryWorkerConfig().Validate() error = nil, want invalid lease budget")
	}
}

func TestOrderServicePaymentRecoveryStartupConfigRejectsInvalidPollInterval(t *testing.T) {
	config := orderServiceConfig{PaymentRecovery: paymentRecoveryConfig{
		PollInterval:  0,
		BatchSize:     20,
		LeaseDuration: 3 * time.Minute,
		CallTimeout:   2 * time.Second,
	}}

	if err := config.validatePaymentRecoveryStartupConfig(); err == nil {
		t.Fatal("validatePaymentRecoveryStartupConfig() error = nil, want invalid poll interval")
	}
}
