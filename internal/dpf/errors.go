// SPDX-License-Identifier: Apache-2.0

// Package dpf は IIJ DNSプラットフォームサービス (DPF) へのアクセスを閉じ込める
// クライアント層である (憲章 原則 II)。
//
// github.com/iij/dpf-go の型・エラー・リトライ・レート制限・排他は本パッケージの内側に
// 閉じ、上位には本パッケージの型とエラーのみを返す。
package dpf

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/misc/k8s"
	"github.com/iij/dpf-go/utils"
)

// Kind は失敗の分類である (FR-010)。
type Kind int

const (
	// Temporary は時間をおいた再試行で解消しうる失敗である。
	Temporary Kind = iota
	// Permanent は設定や状態を直さない限り解消しない失敗である。再試行しない (FR-008)。
	Permanent
)

func (k Kind) String() string {
	if k == Permanent {
		return "permanent"
	}
	return "temporary"
}

// 本パッケージが返す失敗の理由。errors.Is で判別する。
var (
	ErrSecretNotFound        = errors.New("token secret not found")
	ErrSecretKeyNotFound     = errors.New("token key not found in secret")
	ErrSecretEmpty           = errors.New("token secret value is empty")
	ErrSecretForbidden       = errors.New("not permitted to read the token secret")
	ErrKubernetesCredentials = errors.New("kubernetes credentials or namespace unavailable")
	ErrZoneLocked            = errors.New("zone is locked by another holder")
	ErrLockLost              = errors.New("zone lock was lost during the operation")
	ErrZoneLabelLimit        = errors.New("zone label limit reached; the zone lock needs one zone label")
	ErrOwnerTooLong          = errors.New("lock owner is too long")
	ErrZoneNotFound          = errors.New("delegated zone (ResolvedZone) not found in DPF")
	ErrNameOutsideZone       = errors.New("resolved FQDN is not within ResolvedZone")
	ErrDeadline              = errors.New("operation deadline exceeded or canceled")
	ErrAPI                   = errors.New("DPF API returned an error")
	ErrUnavailable           = errors.New("DPF API is unavailable")
)

// Error は本パッケージの失敗である。Detail には原因の特定に必要な全文 (DPF のエラー応答の
// 本文を含む) を保持する (FR-088)。認証情報は含めない (FR-012)。
type Error struct {
	Kind   Kind
	Reason error
	Detail string
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return e.Reason.Error()
	}
	return e.Reason.Error() + ": " + e.Detail
}

// Unwrap は理由を返す。
func (e *Error) Unwrap() error { return e.Reason }

func newError(kind Kind, reason error, format string, args ...any) *Error {
	return &Error{Kind: kind, Reason: reason, Detail: fmt.Sprintf(format, args...)}
}

// IsPermanent は err が恒久的な失敗かを返す。
func IsPermanent(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind == Permanent
}

// Convert は dpf-go と misc/k8s のエラーを本パッケージのエラーへ変換する。
// 元のエラーの型は保持せず、メッセージのみを Detail に残す。
func Convert(err error) error {
	if err == nil {
		return nil
	}
	var own *Error
	if errors.As(err, &own) {
		return own
	}
	msg := err.Error()
	switch {
	case errors.Is(err, k8s.ErrSecretNotFound):
		return newError(Permanent, ErrSecretNotFound, "%s", msg)
	case errors.Is(err, k8s.ErrKeyNotFound):
		return newError(Permanent, ErrSecretKeyNotFound, "%s", msg)
	case errors.Is(err, k8s.ErrEmptySecret):
		return newError(Permanent, ErrSecretEmpty, "%s", msg)
	case errors.Is(err, k8s.ErrForbidden):
		return newError(Permanent, ErrSecretForbidden, "%s", msg)
	case errors.Is(err, k8s.ErrNoCredentials), errors.Is(err, k8s.ErrNamespaceUnknown), errors.Is(err, k8s.ErrNilClient):
		return newError(Permanent, ErrKubernetesCredentials, "%s", msg)
	case errors.Is(err, utils.ErrLabelLimit):
		return newError(Permanent, ErrZoneLabelLimit, "%s", msg)
	case errors.Is(err, utils.ErrOwnerTooLong):
		return newError(Permanent, ErrOwnerTooLong, "%s", msg)
	case errors.Is(err, utils.ErrZoneNotFound):
		return newError(Permanent, ErrZoneNotFound, "%s", msg)
	case errors.Is(err, utils.ErrNotLockHolder):
		return newError(Temporary, ErrLockLost, "%s", msg)
	case errors.Is(err, utils.ErrStillLock):
		return newError(Temporary, ErrZoneLocked, "%s", msg)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return newError(Temporary, ErrDeadline, "%s", msg)
	}
	var apiErr *sdk.GenericOpenAPIError
	if errors.As(err, &apiErr) {
		// エラー応答の本文を全文残す。問い合わせ用の request_id を失わない (FR-088)。
		return newError(Temporary, ErrAPI, "%s: %s", msg, string(apiErr.Body()))
	}
	return newError(Temporary, ErrUnavailable, "%s", msg)
}
