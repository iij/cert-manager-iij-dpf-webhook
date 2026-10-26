// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"errors"

	"github.com/iij/dpf-go/utils"

	"github.com/iij/cert-manager-iij-dpf-webhook/internal/dnsname"
)

// Zone は変更するゾーンである。
type Zone struct {
	ID   string
	Name dnsname.Name
}

// FindZone は ResolvedZone と一致する DPF のゾーンを返す (FR-013〜015 / research.md D-18)。
//
// ResolvedZone は cert-manager が SOA を探索して求めた、実際に委任されているゾーンである。
// チャレンジ対象の FQDN と longest match するゾーンを選ばない。utils.GetZoneFromName は
// longest match であり、一致するゾーンが無いと親ゾーンを返すため、返ったゾーンの名前が
// ResolvedZone と一致することを確かめる。
func (c *Client) FindZone(ctx context.Context, resolvedZone, fqdn dnsname.Name) (Zone, error) {
	if !fqdn.IsSubDomainOf(resolvedZone) {
		return Zone{}, newError(Permanent, ErrNameOutsideZone,
			"fqdn %s, ResolvedZone %s", fqdn, resolvedZone)
	}
	z, err := utils.GetZoneFromName(ctx, c.api.ZonesAPI, resolvedZone.String(), false)
	if errors.Is(err, utils.ErrZoneNotFound) {
		return Zone{}, newError(Permanent, ErrZoneNotFound, "ResolvedZone %s, fqdn %s", resolvedZone, fqdn)
	}
	if err != nil {
		return Zone{}, Convert(err)
	}
	name, err := dnsname.Parse(z.Name)
	if err != nil || !name.Equal(resolvedZone) {
		return Zone{}, newError(Permanent, ErrZoneNotFound,
			"ResolvedZone %s, fqdn %s (DPF has only an enclosing zone)", resolvedZone, fqdn)
	}
	return Zone{ID: z.Id, Name: name}, nil
}
