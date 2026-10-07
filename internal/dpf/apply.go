// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"time"

	sdk "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/utils"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/challenge"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/observability"
)

const (
	// DefaultLockTTL はゾーンロックの保持期間の既定である。dpf-go の既定 15 分から短くする。
	// Apply は実行中に自動で延長するため、操作が長引いても失効しない (D-15)。
	DefaultLockTTL = 2 * time.Minute
	// DefaultLockWait はゾーンロックを取得できない場合に再試行する間隔の既定である。
	// 待機の上限は操作期限である (FR-054)。
	DefaultLockWait = 5 * time.Second
	// ChallengeTTL はチャレンジレコードを新たに作成するときの TTL (秒) である (FR-032)。
	ChallengeTTL int32 = 60
)

// ApplyOptions はゾーンロックの設定である。
type ApplyOptions struct {
	// Owner はロックオーナー (配備単位の必須設定) である。1〜32 文字。
	Owner    string
	LockTTL  time.Duration
	LockWait time.Duration
}

// Planner は排他の内側で読んだ対象名の TXT から計画を導く。
type Planner func(cur *challenge.Record) (challenge.Result, error)

// Description はゾーン反映に付けるコメントである (FR-064)。切り詰めない。
func Description(owner string) string {
	return "by cert-manager(" + owner + ")"
}

// Apply はゾーンロックの下で対象名の TXT を読み、plan の結果をゾーンへ反映する
// (D-11 / D-13)。反映したかどうかを返す。
//
//   - 一括置き換えは utils.ZoneApplier に委ね、PatchZoneAtomicChanges を直接呼ばない。
//     SOA と Zone Apex の NS は取り込まれない。
//   - 排他は既定のゾーンのラベルを用いる実装とし、utils.WithLocker で差し替えない (D-16)。
//   - 対象名の TXT 以外のレコードは、受け取った要素をそのまま返す。
//   - 計画が変更不要なら utils.ErrSkipApply を返し、反映を省く (FR-084a)。
//   - 書き込む直前に recheck を呼ぶ。domainFilters の再判定に用いる (FR-018)。
//   - 反映の後に解放だけが失敗した場合も失敗として返す (FR-059)。
func (c *Client) Apply(ctx context.Context, zone Zone, fqdn dnsname.Name, o ApplyOptions,
	recheck func() error, plan Planner,
) (bool, error) {
	applier := utils.NewZoneApplier(c.api.RecordsAPI, c.api.ZonesAPI, c.api.JobsAPI, zone.ID,
		utils.WithLockOptions(utils.WithOwner(o.Owner), utils.WithTTL(o.LockTTL)),
		utils.WithHoldOptions(utils.WithLockWait(o.LockWait)),
	)

	log := observability.Logger(ctx).With("zone", zone.Name.String(), "zoneID", zone.ID, "owner", o.Owner)
	var ownErr error
	applied, locked := false, false
	var change []any // 加除の差分 (FR-088a / FR-089)
	edit := func(ctx context.Context, records []sdk.OverwriteRecordsInner) ([]sdk.OverwriteRecordsInner, error) {
		// edit はゾーンロックの保持中にのみ呼ばれる (FR-065)。
		if !locked {
			locked = true
			log.Info("zone lock acquired")
		}
		idx := -1
		for i := range records {
			if records[i].Rrtype == sdk.RECORDSRRTYPE_TXT && sameName(records[i].Name, fqdn) {
				idx = i
				break
			}
		}
		var cur *challenge.Record
		if idx >= 0 {
			cur = &challenge.Record{Values: rdataValues(records[idx].Rdata), Labels: maps.Clone(records[idx].Labels)}
		}
		res, err := plan(cur)
		if err != nil {
			ownErr = err
			return nil, err
		}
		if !res.Changed {
			return nil, utils.ErrSkipApply
		}
		if err := recheck(); err != nil {
			ownErr = err
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		applied = true
		out := replaceChallenge(records, idx, fqdn, res)
		change = diff(cur, res.Record, idx < 0)
		change = append(change, "name", fqdn.String(), "records", len(out))
		return out, nil
	}

	err := applier.Apply(ctx, edit, utils.WithApplyDescription(Description(o.Owner)))
	if errors.Is(err, utils.ErrSkipApply) {
		err = nil
	}
	if locked {
		if err == nil {
			log.Info("zone lock released")
		} else {
			log.Warn("zone lock released or left to expire by its TTL after a failure", "error", err)
		}
	}
	if ownErr != nil {
		return false, ownErr
	}
	if err != nil {
		err = Convert(err)
	}
	if change != nil {
		result, level := "success", slog.LevelInfo
		if err != nil {
			result, level = "failure", slog.LevelWarn
		}
		log.Log(ctx, level, "DNS record change",
			append(change, "description", Description(o.Owner), "result", result)...)
	}
	return applied && err == nil, err
}

// diff は対象名の TXT の変更を、操作種別と加除した RDATA・ラベルで表す。
func diff(before *challenge.Record, after *challenge.Record, created bool) []any {
	op := "update"
	switch {
	case created:
		op = "create"
	case after == nil:
		op = "delete"
	}
	var bv, av []string
	bl, al := map[string]string{}, map[string]string{}
	if before != nil {
		bv, bl = before.Values, before.Labels
	}
	if after != nil {
		av, al = after.Values, after.Labels
	}
	return []any{"operation", op,
		"addedRdata", minus(av, bv), "removedRdata", minus(bv, av),
		"addedLabels", labelDiff(al, bl), "removedLabels", labelDiff(bl, al)}
}

func minus(a, b []string) []string {
	out := []string{}
	for _, v := range a {
		if !slices.Contains(b, v) {
			out = append(out, v)
		}
	}
	return out
}

// labelDiff は a にあって b に無い、または値の異なるラベルを "key=value" で返す。
func labelDiff(a, b map[string]string) []string {
	out := []string{}
	for _, k := range slices.Sorted(maps.Keys(a)) {
		if bv, ok := b[k]; !ok || bv != a[k] {
			out = append(out, k+"="+a[k])
		}
	}
	return out
}

// replaceChallenge は対象名の TXT を計画の結果に置き換えた一覧を返す。
// 対象以外の要素は受け取ったものをそのまま返す。
func replaceChallenge(records []sdk.OverwriteRecordsInner, idx int, fqdn dnsname.Name,
	res challenge.Result,
) []sdk.OverwriteRecordsInner {
	out := make([]sdk.OverwriteRecordsInner, 0, len(records)+1)
	for i := range records {
		if i != idx {
			out = append(out, records[i])
			continue
		}
		if res.Record == nil {
			continue // RDATA が空になる場合はレコードごと削除する (FR-035)。
		}
		r := records[i] // TTL とコメントは維持する (FR-032)。
		r.Rdata = toRdata(res.Record.Values)
		r.Labels = maps.Clone(res.Record.Labels)
		out = append(out, r)
	}
	if idx < 0 && res.Record != nil {
		ttl := ChallengeTTL
		out = append(out, sdk.OverwriteRecordsInner{
			Name:   fqdn.String(),
			Ttl:    *sdk.NewNullableInt32(&ttl),
			Rrtype: sdk.RECORDSRRTYPE_TXT,
			Rdata:  toRdata(res.Record.Values),
			Labels: maps.Clone(res.Record.Labels),
		})
	}
	return out
}

func toRdata(values []string) []sdk.RecordsRdataInner {
	out := make([]sdk.RecordsRdataInner, 0, len(values))
	for _, v := range values {
		out = append(out, sdk.RecordsRdataInner{Value: sdk.PtrString(v)})
	}
	return out
}
