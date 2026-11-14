package siteingest

import (
	"context"

	"github.com/syncaitechnologies/SyncamAi/backend/internal/alerting"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/device"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/tenant"
	"github.com/syncaitechnologies/SyncamAi/backend/internal/zones"
)

// NewRepositoryDescriber resolves site, camera and zone names for messages.
// Any lookup failure simply leaves that name out of the notification.
func NewRepositoryDescriber(sites tenant.Repository, cameras device.Repository, zoneRepository zones.Repository) DescribeFunc {
	return func(ctx context.Context, alert alerting.Alert) AlertLabels {
		labels := AlertLabels{}
		if sites != nil {
			if listed, err := sites.ListSites(ctx, alert.TenantID); err == nil {
				for _, site := range listed {
					if site.ID == alert.SiteID {
						labels.SiteName, labels.Timezone = site.Name, site.Timezone
					}
				}
			}
		}
		if cameras != nil {
			if camera, err := cameras.Get(ctx, alert.TenantID, alert.CameraID); err == nil {
				labels.CameraName = camera.Name
			}
		}
		if zoneRepository != nil && alert.ZoneID != alert.CameraID {
			if zone, err := zoneRepository.Get(ctx, alert.TenantID, alert.ZoneID); err == nil {
				labels.ZoneName = zone.Name
			}
		}
		return labels
	}
}

// SiteNamer returns a function that resolves a site's display name.
func SiteNamer(sites tenant.Repository) func(context.Context, string, string) string {
	return func(ctx context.Context, tenantID, siteID string) string {
		if sites == nil {
			return ""
		}
		listed, err := sites.ListSites(ctx, tenantID)
		if err != nil {
			return ""
		}
		for _, site := range listed {
			if site.ID == siteID {
				return site.Name
			}
		}
		return ""
	}
}
