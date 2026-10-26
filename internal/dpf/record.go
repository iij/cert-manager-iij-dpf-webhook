// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"maps"

	sdk "github.com/iij/dpf-go"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/challenge"
	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dnsname"
)

// ReadChallenge は排他を取らずに、公開されている対象名の TXT を読む (D-11 の 2a)。
// レコードが無ければ nil を返す。
//
// 変更の要否の判定にのみ用いる。判定の結果が「不要」であれば何も変更しないため、
// 読み出しの時点の状態が古くても害がない (FR-084)。
func (c *Client) ReadChallenge(ctx context.Context, zone Zone, fqdn dnsname.Name) (*challenge.Record, error) {
	list, _, err := c.api.RecordsAPI.GetRecordCurrents(ctx, zone.ID).
		KeywordsName([]string{fqdn.String()}).
		KeywordsRrtype(sdk.RECORDSRRTYPE_TXT).
		ExecuteAll()
	if err != nil {
		return nil, Convert(err)
	}
	if list == nil {
		return nil, nil
	}
	for i := range list.Results {
		r := &list.Results[i]
		if r.Rrtype != sdk.RECORDSRRTYPE_TXT || !sameName(r.Name, fqdn) {
			continue
		}
		return &challenge.Record{Values: rdataValues(r.Rdata), Labels: maps.Clone(r.Labels)}, nil
	}
	return nil, nil
}

// sameName は DPF が返した名前が fqdn と一致するかを正規化名で判定する。
func sameName(name string, fqdn dnsname.Name) bool {
	n, err := dnsname.Parse(name)
	return err == nil && n.Equal(fqdn)
}

func rdataValues(in []sdk.RecordsRdataInner) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v.Value != nil {
			out = append(out, *v.Value)
		}
	}
	return out
}
