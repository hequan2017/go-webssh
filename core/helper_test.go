package core

import (
	"net/http/httptest"
	"testing"
)

func TestPositiveInt(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		fallback int
		want     int
		wantErr  bool
	}{
		{name: "默认值", value: "", fallback: 120, want: 120},
		{name: "有效值", value: "80", fallback: 120, want: 80},
		{name: "非数字", value: "abc", fallback: 120, wantErr: true},
		{name: "零", value: "0", fallback: 120, wantErr: true},
		{name: "过大", value: "1001", fallback: 120, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := positiveInt(tt.value, tt.fallback)
			if (err != nil) != tt.wantErr {
				t.Fatalf("positiveInt() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("positiveInt() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSameOrigin(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{name: "无 Origin", want: true},
		{name: "同源", origin: "http://example.com", want: true},
		{name: "跨域", origin: "https://other.example", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com/ws/default", nil)
			req.Header.Set("Origin", tt.origin)
			if got := sameOrigin(req); got != tt.want {
				t.Fatalf("sameOrigin() = %v, want %v", got, tt.want)
			}
		})
	}
}
