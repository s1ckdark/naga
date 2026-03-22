package domain

import (
	"testing"
)

func TestDeviceMetrics_HasError(t *testing.T) {
	m := &DeviceMetrics{}
	if m.HasError() {
		t.Error("empty metrics should not have error")
	}

	m.Error = "collection failed"
	if !m.HasError() {
		t.Error("metrics with error should return true")
	}
}
