// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"errors"
	"maps"
	"time"

	sdk "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/utils"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/challenge"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dnsname"
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

	var ownErr error
	applied := false
	edit := func(ctx context.Context, records []sdk.OverwriteRecordsInner) ([]sdk.OverwriteRecordsInner, error) {
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
		return replaceChallenge(records, idx, fqdn, res), nil
	}

	err := applier.Apply(ctx, edit, utils.WithApplyDescription(Description(o.Owner)))
	if ownErr != nil {
		return false, ownErr
	}
	if err != nil {
		if errors.Is(err, utils.ErrSkipApply) {
			return false, nil
		}
		return applied, Convert(err)
	}
	return applied, nil
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
