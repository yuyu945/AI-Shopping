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
	if got.BatchSize != 20 || got.LeaseDuration != 30*time.Second || got.CallTimeout != 2*time.Second {
		t.Fatalf("paymentRecoveryWorkerConfig() = %#v", got)
	}
	if serviceConfig.PaymentRecovery.PollInterval != time.Second {
		t.Fatalf("PaymentRecovery.PollInterval = %s, want 1s", serviceConfig.PaymentRecovery.PollInterval)
	}
}
