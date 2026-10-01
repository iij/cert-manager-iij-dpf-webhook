// SPDX-License-Identifier: Apache-2.0

package challenge

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"
)

// Action は要求の種類である。
type Action int

const (
	// Present はチャレンジコードを置く。
	Present Action = iota + 1
	// CleanUp は後片付けである。期限切れの回収のみを行う (FR-082)。
	CleanUp
)

// ErrLabelLimit はラベルの総数が上限を超える場合に返される。他者のラベルも本サービスの
// 記録も追い出さない (FR-086)。
var ErrLabelLimit = errors.New("challenge: record label limit reached")

// ErrInvalidRequest は要求が不正な場合に返される。
var ErrInvalidRequest = errors.New("challenge: invalid request")

// Record はチャレンジレコード (対象名の TXT) の状態である。nil は「レコードが無い」を表す。
type Record struct {
	// Values は RDATA の値 (TXT の表記) である。本サービス以外の値を含みうる。
	Values []string
	// Labels はレコードのラベルである。本サービス以外のラベルを含みうる。
	Labels map[string]string
}

// Request は 1 回の要求である。
type Request struct {
	Action Action
	// Key はチャレンジコードである。
	Key string
	// Now は判定の基準時刻である。
	Now time.Time
	// Grace は期限の猶予である (D-06)。
	Grace time.Duration
	// Threshold は期限を更新する閾値である。残りがこれ以上なら更新しない (FR-079)。
	Threshold time.Duration
}

// Result は計画である。
type Result struct {
	// Changed は現状から変える必要があることを示す。false なら変更 API を呼ばない (FR-083)。
	Changed bool
	// Record はあるべき状態である。nil はレコードを削除する (または無いままにする) ことを示す。
	Record *Record
	// Created はレコードを新たに作成することを示す。TTL を定めるのはこの場合に限る (FR-032)。
	Created bool
	// Reclaimed は期限切れとして取り除いた RDATA の数である。
	Reclaimed int
}

func (r Request) validate() error {
	switch {
	case r.Action != Present && r.Action != CleanUp:
		return fmt.Errorf("%w: unknown action %d", ErrInvalidRequest, r.Action)
	case r.Key == "":
		return fmt.Errorf("%w: empty key", ErrInvalidRequest)
	case r.Grace <= 0 || r.Threshold <= 0 || r.Threshold >= r.Grace:
		return fmt.Errorf("%w: threshold must be positive and shorter than grace", ErrInvalidRequest)
	}
	return nil
}

// Plan は現状 cur と要求 r から計画を導く。cur を書き換えない。
//
// 手順は contracts/dpf-records.md §3 のとおりである。
//
//  1. 期限切れの回収: 本サービスのラベルのうち期限を過ぎたものを取り除き、対応する
//     RDATA (値から計算した識別値が一致するもの) も取り除く。対応するラベルを持たない
//     RDATA は取り除かない (FR-072〜075)。
//  2. Present の場合のみ、当該コードの RDATA を追加し (既にあれば追加しない)、ラベルが
//     無いか残りが閾値を下回れば期限を「現在時刻 + 猶予」にする (FR-077〜079)。
//
// 期限の値を解釈できない本サービスのラベルは取り除くが、対応する RDATA は残す。
// 期限切れであると確かめられないものを削除しないためである。
func Plan(cur *Record, r Request) (Result, error) {
	if err := r.validate(); err != nil {
		return Result{}, err
	}

	var values []string
	labels := map[string]string{}
	if cur != nil {
		values = slices.Clone(cur.Values)
		maps.Copy(labels, cur.Labels)
	}

	// 1. 期限切れの回収。
	expired := map[string]bool{}
	for k, v := range labels {
		id, ok := ParseLabelKey(k)
		if !ok {
			continue
		}
		at, err := DecodeExpiry(v)
		if err != nil {
			delete(labels, k)
			continue
		}
		if !r.Now.Before(at) {
			delete(labels, k)
			expired[id] = true
		}
	}
	reclaimed := 0
	kept := values[:0:0]
	for _, v := range values {
		if code, ok := UnquoteTXT(v); ok && expired[TargetID(code)] {
			reclaimed++
			continue
		}
		kept = append(kept, v)
	}

	// 2. 提示。
	if r.Action == Present {
		own := QuoteTXT(r.Key)
		if !slices.Contains(kept, own) {
			kept = append(kept, own)
		}
		key := LabelKey(TargetID(r.Key))
		renew := true
		if v, ok := labels[key]; ok {
			if at, err := DecodeExpiry(v); err == nil && at.Sub(r.Now) >= r.Threshold {
				renew = false
			}
		}
		if renew {
			labels[key] = EncodeExpiry(r.Now.Add(r.Grace))
		}
	}

	var next *Record
	if len(kept) > 0 {
		next = &Record{Values: kept, Labels: labels}
	}
	if next != nil && len(next.Labels) > MaxLabels {
		return Result{}, fmt.Errorf("%w: %d labels", ErrLabelLimit, len(next.Labels))
	}

	return Result{
		Changed:   !equalRecord(cur, next),
		Record:    next,
		Created:   cur == nil && next != nil,
		Reclaimed: reclaimed,
	}, nil
}

// equalRecord は 2 つの状態が等しいかを返す。ラベルの nil と空は区別しない。
func equalRecord(a, b *Record) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if !slices.Equal(a.Values, b.Values) {
		return false
	}
	if len(a.Labels) != len(b.Labels) {
		return false
	}
	for k, v := range a.Labels {
		if bv, ok := b.Labels[k]; !ok || bv != v {
			return false
		}
	}
	return true
}
