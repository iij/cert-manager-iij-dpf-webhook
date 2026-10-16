// SPDX-License-Identifier: Apache-2.0

package dpf_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	sdk "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/misc/k8s"
	"github.com/iij/dpf-go/utils"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dpf"
)

func TestConvertClassifies(t *testing.T) {
	cases := []struct {
		name      string
		in        error
		reason    error
		permanent bool
	}{
		{"Secret 不在", &utils.TokenError{Err: k8s.ErrSecretNotFound}, dpf.ErrSecretNotFound, true},
		{"key 不在", fmt.Errorf("wrap: %w", k8s.ErrKeyNotFound), dpf.ErrSecretKeyNotFound, true},
		{"値が空", k8s.ErrEmptySecret, dpf.ErrSecretEmpty, true},
		{"読み取り権限なし", k8s.ErrForbidden, dpf.ErrSecretForbidden, true},
		{"資格情報なし", k8s.ErrNoCredentials, dpf.ErrKubernetesCredentials, true},
		{"排他を保持されている", utils.ErrStillLock, dpf.ErrZoneLocked, false},
		{"排他を失った", utils.ErrNotLockHolder, dpf.ErrLockLost, false},
		{"ゾーンのラベルの枠が足りない", utils.ErrLabelLimit, dpf.ErrZoneLabelLimit, true},
		{"保持者が長すぎる", utils.ErrOwnerTooLong, dpf.ErrOwnerTooLong, true},
		{"操作期限", context.DeadlineExceeded, dpf.ErrDeadline, false},
		{"到達失敗", errors.New("dial tcp: connection refused"), dpf.ErrUnavailable, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := dpf.Convert(c.in)
			if !errors.Is(got, c.reason) {
				t.Fatalf("Convert(%v) = %v, want %v", c.in, got, c.reason)
			}
			if dpf.IsPermanent(got) != c.permanent {
				t.Errorf("IsPermanent = %v, want %v", dpf.IsPermanent(got), c.permanent)
			}
			assertNoSDKTypes(t, got)
		})
	}
}

func TestConvertNil(t *testing.T) {
	if dpf.Convert(nil) != nil {
		t.Fatal("Convert(nil) != nil")
	}
}

// API のエラー応答は全文を保持し、dpf-go の型を漏らさない (FR-088 / 原則 II)。
func TestConvertAPIErrorKeepsBody(t *testing.T) {
	srv := newErrorServer(t, `{"request_id":"REQID123","error_type":"ParameterError","error_message":"bad"}`)
	_, _, err := sdk.NewAPIClient(srv).ZonesAPI.GetZoneList(context.Background()).Execute()
	if err == nil {
		t.Fatal("expected an error")
	}
	got := dpf.Convert(err)
	if !errors.Is(got, dpf.ErrAPI) {
		t.Fatalf("Convert = %v, want ErrAPI", got)
	}
	var de *dpf.Error
	if !errors.As(got, &de) || de.Detail == "" {
		t.Fatalf("detail missing: %#v", got)
	}
	if want := "REQID123"; !contains(de.Detail, want) {
		t.Errorf("detail %q does not contain %q", de.Detail, want)
	}
	assertNoSDKTypes(t, got)
}

// 無効なトークンは恒久的な失敗とし、その他のエラー応答は一時的な失敗とする (research.md D-23)。
// 本文は DPF-API リファレンス 2.3 と、実環境の応答 (T100) に倣う。
func TestConvertAPIErrorClassifiesInvalidToken(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		permanent bool
	}{
		{"無効なトークン", `{"request_id":"R1","error_type":"ParameterError","error_message":"There are invalid parameters.","error_details":[{"code":"invalid","attribute":"access_token"}]}`, true},
		{"他のパラメータの誤り", `{"request_id":"R2","error_type":"ParameterError","error_message":"There are invalid parameters.","error_details":[{"code":"invalid","attribute":"name"}]}`, false},
		{"JSON の解析の失敗", `{"request_id":"R3","error_type":"ParameterError","error_message":"JSON parse error occurred."}`, false},
		{"本文が JSON でない", `<html>bad gateway</html>`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := sdk.NewAPIClient(newErrorServer(t, c.body)).ZonesAPI.GetZoneList(context.Background()).Execute()
			if err == nil {
				t.Fatal("expected an error")
			}
			got := dpf.Convert(err)
			if !errors.Is(got, dpf.ErrAPI) {
				t.Fatalf("Convert = %v, want ErrAPI", got)
			}
			if dpf.IsPermanent(got) != c.permanent {
				t.Errorf("IsPermanent = %v, want %v", dpf.IsPermanent(got), c.permanent)
			}
			if dpf.IsPermanent(dpf.Public(got)) != c.permanent {
				t.Errorf("Public で分類が変わった")
			}
			assertNoSDKTypes(t, got)
		})
	}
}

func assertNoSDKTypes(t *testing.T, err error) {
	t.Helper()
	var apiErr *sdk.GenericOpenAPIError
	if errors.As(err, &apiErr) {
		t.Errorf("dpf-go の型が漏れている: %T", apiErr)
	}
	var tokErr *utils.TokenError
	if errors.As(err, &tokErr) {
		t.Errorf("dpf-go の型が漏れている: %T", tokErr)
	}
	for _, s := range []error{utils.ErrStillLock, utils.ErrNotLockHolder, utils.ErrLabelLimit, utils.ErrOwnerTooLong} {
		if errors.Is(err, s) {
			t.Errorf("dpf-go の番兵が漏れている: %v", s)
		}
	}
}
