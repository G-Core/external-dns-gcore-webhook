/*
Copyright 2017 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gcoreprovider

import (
	"context"
	"fmt"
	"strings"
	"time"

	gdns "github.com/G-Core/gcore-dns-sdk-go"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
)

const (
	ProviderName = "gcore"
	EnvAPIToken  = "GCORE_PERMANENT_API_TOKEN"
	logDryRun    = "[DryRun] "
	maxTimeout   = 60 * time.Second
)

type dnsManager interface {
	AddZoneRRSet(ctx context.Context,
		zone, recordName, recordType string,
		values []gdns.ResourceRecord, ttl int, opts ...gdns.AddZoneOpt) error
	ZonesWithRecords(ctx context.Context, filters ...func(zone *gdns.ZonesFilter)) ([]gdns.Zone, error)
	Zones(ctx context.Context, filters ...func(zone *gdns.ZonesFilter)) ([]gdns.Zone, error)
	DeleteRRSetRecord(ctx context.Context, zone, name, recordType string, contents ...string) error
}

type DnsProvider struct {
	provider.BaseProvider
	client dnsManager
	dryRun bool
}

func NewProvider(domainFilter endpoint.DomainFilter, apiKey string, dryRun bool) (*DnsProvider, error) {
	log.Infof("%s: starting init provider: filters=%+v , dryRun=%v",
		ProviderName, domainFilter.Filters, dryRun)
	defer log.Infof("%s: finishing init provider", ProviderName)
	if apiKey == "" {
		return nil, EnvError("empty " + EnvAPIToken)
	}
	p := &DnsProvider{
		client: gdns.NewClient(gdns.PermanentAPIKeyAuth(apiKey)),
		dryRun: dryRun,
	}

	return p, nil
}

func (p *DnsProvider) Records(rootCtx context.Context) ([]*endpoint.Endpoint, error) {
	log.Infof("%s: starting get records", ProviderName)
	filters := make([]func(*gdns.ZonesFilter), 0, 1)
	if len(p.GetDomainFilter().Filters) > 0 {
		filters = append(filters, func(filter *gdns.ZonesFilter) {
			filter.Names = p.GetDomainFilter().Filters
		})
	}
	ctx, cancel := p.ctxWithMyTimeout(rootCtx)
	defer cancel()
	zs, err := p.client.ZonesWithRecords(ctx, filters...)
	if err != nil {
		return nil, fmt.Errorf("%s: records: %w", ProviderName, err)
	}
	result := make([]*endpoint.Endpoint, 0)
	for _, z := range zs {
		for _, r := range z.Records {
			if !provider.SupportedRecordType(r.Type) {
				continue
			}
			result = append(result,
				endpoint.NewEndpointWithTTL(r.Name, r.Type, endpoint.TTL(r.TTL), r.ShortAnswers...))
		}
	}
	defer log.Debugf("%s: finishing get records: %d", ProviderName, len(result))
	log.Debugf("%s: records: %v", ProviderName, result)
	return result, nil
}

func (p *DnsProvider) ApplyChanges(rootCtx context.Context, changes *plan.Changes) error {
	if !changes.HasChanges() {
		return nil
	}
	log.Infof("%s: ApplyChanges createLen=%d, deleteLen=%d, updateOldLen=%d, updateNewLen=%d",
		ProviderName, len(changes.Create), len(changes.Delete), len(changes.UpdateOld), len(changes.UpdateNew))
	ctx, cancel := p.ctxWithMyTimeout(rootCtx)
	defer cancel()
	gr1, _ := errgroup.WithContext(ctx)
	gr2, _ := errgroup.WithContext(ctx)
	extractZone := p.zoneFromDNSNameGetter()
	appliedChanges := struct {
		created uint
		deleted uint
		updated uint
	}{}
	// prepare zone to add changes by removing outdated records
	for _, d := range changes.UpdateNew {
		d := d
		zone := extractZone(d.DNSName)
		if zone == "" {
			continue
		}
		recordValues := make([]string, 0)
		errMsg := make([]string, 0)
		// find content diff to delete
		for _, content := range unexistingTargets(d, changes.UpdateOld, false) {
			appliedChanges.updated++
			msg := fmt.Sprintf("update old %s %s %s",
				d.DNSName, d.RecordType, content)
			if p.dryRun {
				log.Info(logDryRun + msg)
				continue
			}
			log.Debug(msg)
			recordValues = append(recordValues, content)
			errMsg = append(errMsg, msg)
		}
		if len(recordValues) == 0 {
			continue
		}
		gr2.Go(func() error {
			err := errSafeWrap(strings.Join(errMsg, "; "),
				p.client.DeleteRRSetRecord(ctx, zone, d.DNSName, d.RecordType, recordValues...))
			log.Debugf("%s ApplyChanges.updateNew,DeleteRRSetRecord: %s %s %v ERR=%s",
				ProviderName, d.DNSName, d.RecordType, recordValues, err)
			return err
		})
	}
	// remove deleted records
	for _, d := range changes.Delete {
		d := d
		zone := extractZone(d.DNSName)
		if zone == "" {
			continue
		}
		recordValues := make([]string, 0)
		errMsg := make([]string, 0)
		for _, content := range d.Targets {
			appliedChanges.deleted++
			msg := fmt.Sprintf("delete %s %s %s",
				d.DNSName, d.RecordType, content)
			if p.dryRun {
				log.Info(logDryRun + msg)
				continue
			}
			log.Debug(msg)
			recordValues = append(recordValues, content)
			errMsg = append(errMsg, msg)
		}
		gr1.Go(func() error {
			err := errSafeWrap(strings.Join(errMsg, "; "),
				p.client.DeleteRRSetRecord(ctx, zone, d.DNSName, d.RecordType, recordValues...))
			log.Debugf("%s ApplyChanges.Delete,DeleteRRSetRecord: %s %s %v ERR=%s",
				ProviderName, d.DNSName, d.RecordType, recordValues, err)
			return err
		})
	}
	// add created records
	for _, c := range changes.Create {
		c := c
		zone := extractZone(c.DNSName)
		if zone == "" {
			continue
		}
		recordValues := make([]gdns.ResourceRecord, 0)
		errMsg := make([]string, 0)
		for _, content := range c.Targets {
			appliedChanges.created++
			msg := fmt.Sprintf("create %s %s %s", c.DNSName, c.RecordType, content)
			if p.dryRun {
				log.Info(logDryRun + msg)
				continue
			}
			log.Debug(msg)
			rr := gdns.ResourceRecord{Enabled: true}
			rr.SetContent(c.RecordType, content)
			recordValues = append(recordValues, rr)
			errMsg = append(errMsg, msg)
		}
		gr1.Go(func() error {
			err := errSafeWrap(strings.Join(errMsg, "; "),
				p.client.AddZoneRRSet(ctx, zone, c.DNSName, c.RecordType, recordValues, int(c.RecordTTL)))
			log.Debugf("%s ApplyChanges.Create,AddZoneRRSet: %s %s %v ERR=%s",
				ProviderName, c.DNSName, c.RecordType, recordValues, err)
			return err
		})
	}
	// wait preparing before send updates to records
	err := gr2.Wait()
	if err != nil {
		return fmt.Errorf("%s: apply changes: %w", ProviderName, err)
	}
	// add changes
	for _, c := range changes.UpdateNew {
		c := c
		zone := extractZone(c.DNSName)
		if zone == "" {
			continue
		}
		recordValues := make([]gdns.ResourceRecord, 0)
		errMsg := make([]string, 0)
		// find content diff to add
		for _, content := range unexistingTargets(c, changes.UpdateOld, true) {
			appliedChanges.updated++
			msg := fmt.Sprintf("update new %s %s %s", c.DNSName, c.RecordType, content)
			if p.dryRun {
				log.Info(logDryRun + msg)
				continue
			}
			log.Debug(msg)
			rr := gdns.ResourceRecord{Enabled: true}
			rr.SetContent(c.RecordType, content)
			recordValues = append(recordValues, rr)
			errMsg = append(errMsg, msg)
		}
		if len(recordValues) == 0 {
			continue
		}
		gr1.Go(func() error {
			err := errSafeWrap(strings.Join(errMsg, "; "),
				p.client.AddZoneRRSet(ctx, zone, c.DNSName, c.RecordType, recordValues, int(c.RecordTTL)))
			log.Debugf("%s ApplyChanges.UpdateNew,AddZoneRRSet: %s %s %v ERR=%s",
				ProviderName, c.DNSName, c.RecordType, recordValues, err)
			return err
		})
	}
	err = gr1.Wait()
	if err != nil {
		return fmt.Errorf("%s: apply changes: %w", ProviderName, err)
	}
	log.Infof("%s: finishing apply changes created=%d, deleted=%d, updated=%d",
		ProviderName, appliedChanges.created, appliedChanges.deleted, appliedChanges.updated)
	return nil
}

func (p *DnsProvider) GetDomainFilter() endpoint.DomainFilter {
	log.Debugf("%s: GetDomainFilter", ProviderName)
	zs, err := p.client.ZonesWithRecords(context.Background())
	if err != nil {
		log.Errorf("%s: ERROR GetDomainFilter: %v", ProviderName, err)
		return endpoint.DomainFilter{}
	}
	domains := make([]string, 0)
	for _, z := range zs {
		domains = append(domains, z.Name, "."+z.Name)
	}
	defer log.Debugf("%s: GetDomainFilter: %+v", ProviderName, domains)
	return endpoint.NewDomainFilter(domains)
}

func (p *DnsProvider) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	return endpoints, nil
}

func (p *DnsProvider) PropertyValuesEqual(_ string, previous string, current string) bool {
	return previous == current
}

func (p *DnsProvider) zoneFromDNSNameGetter() func(name string) (zone string) {
	existingZones := p.GetDomainFilter()
	search := make(map[string]string)
	for _, zone := range existingZones.Filters {
		search[zone] = strings.Trim(zone, ".")
	}
	return func(name string) (zone string) {
		for _, possibleZone := range extractAllZones(name) {
			if result, ok := search[possibleZone]; ok {
				return result
			}
		}
		return ""
	}
}

func (p *DnsProvider) ctxWithMyTimeout(rootCtx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), maxTimeout)
	go func() {
		select {
		case <-rootCtx.Done():
			ctxErr := rootCtx.Err()
			if ctxErr != nil && strings.Contains(ctxErr.Error(), "deadline exceeded") {
				return
			}
			log.Warningf("%s: ctx done: %v", ProviderName, ctxErr)
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func extractAllZones(dnsName string) []string {
	parts := strings.Split(strings.Trim(dnsName, "."), ".")
	if len(parts) < 2 {
		return nil
	}

	var zones []string
	for i := 0; i < len(parts)-1; i++ {
		zones = append(zones, strings.Join(parts[i:], "."))
	}

	return zones
}

func errSafeWrap(msg string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

func unexistingTargets(existing *endpoint.Endpoint,
	toCompare []*endpoint.Endpoint, diffFromExisting bool) endpoint.Targets {
	for _, compare := range toCompare {
		if compare.RecordType != existing.RecordType || compare.DNSName != existing.DNSName {
			continue
		}
		result := endpoint.Targets{}
		if diffFromExisting {
			for _, fromTar := range existing.Targets {
				exist := false
				for _, curTar := range compare.Targets {
					if curTar == fromTar {
						exist = true
						break
					}
				}
				if exist {
					continue
				}
				result = append(result, fromTar)
			}
		} else {
			for _, fromTar := range compare.Targets {
				exist := false
				for _, curTar := range existing.Targets {
					if curTar == fromTar {
						exist = true
						break
					}
				}
				if exist {
					continue
				}
				result = append(result, fromTar)
			}
		}
		return result
	}
	return nil
}

// EnvError description
type EnvError string

func (e EnvError) Error() string {
	return fmt.Sprintf("invalid evirement var: %s", string(e))
}
