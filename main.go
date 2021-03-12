package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jessevdk/go-flags"
	"github.com/linode/linodego"
	"golang.org/x/oauth2"
)

var options struct {
	ClusterID     int `long:"cluster-id" required:"true"`
	ClusterPoolID int `long:"cluster-pool-id" required:"true"`
	DomainID      int `long:"domain-id" required:"true"`
}

func main() {
	_, err := flags.Parse(&options)
	if err != nil {
		log.Fatal(err)
	}

	apiKey, ok := os.LookupEnv("LINODE_TOKEN")
	if !ok {
		log.Fatal("Could not find LINODE_TOKEN, please ensure it is set.")
	}
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: apiKey})

	oauth2Client := &http.Client{
		Transport: &oauth2.Transport{
			Source: tokenSource,
		},
	}

	lc := linodego.NewClient(oauth2Client)

	last := ""

	ctx := context.Background()
	for {
		time.Sleep(5 * time.Minute)
		ipv4, ipv6, err := getIPs(ctx, lc)
		if err != nil {
			log.Println(err)
			continue
		}
		new := strings.Join(ipv4, ",") + "," + strings.Join(ipv6, ",")
		log.Println(new)
		if new == last {
			continue
		}
		last = new
		err = setDNS(ctx, lc, ipv4, ipv6)
		if err != nil {
			log.Println(err)
			continue
		}
	}
}

func getIPs(ctx context.Context, lc linodego.Client) ([]string, []string, error) {
	pool, err := lc.GetLKEClusterPool(ctx, options.ClusterID, options.ClusterPoolID)
	if err != nil {
		return nil, nil, err
	}

	ipsv4 := []string{}
	ipsv6 := []string{}

	for _, l := range pool.Linodes {
		ips, err := lc.GetInstanceIPAddresses(ctx, l.InstanceID)
		if err != nil {
			return nil, nil, err
		}
		for _, ip := range ips.IPv4.Public {
			ipsv4 = append(ipsv4, ip.Address)
		}
		if ips.IPv6.SLAAC.Public {
			ipsv6 = append(ipsv6, ips.IPv6.SLAAC.Address)
		}
	}
	sort.Strings(ipsv4)
	sort.Strings(ipsv6)
	return ipsv4, ipsv6, nil
}

func setDNS(ctx context.Context, lc linodego.Client, ipv4, ipv6 []string) error {
	records, err := lc.ListDomainRecords(ctx, options.DomainID, nil)
	if err != nil {
		return err
	}
	goalv4 := map[string]struct{}{}
	goalv6 := map[string]struct{}{}
	for _, ip := range ipv4 {
		goalv4[ip] = struct{}{}
	}
	for _, ip := range ipv6 {
		goalv6[ip] = struct{}{}
	}
	existv4 := map[string]struct{}{}
	existv6 := map[string]struct{}{}
	for _, r := range records {
		if r.Type == linodego.RecordTypeA && r.Name == "" {
			if _, ok := goalv4[r.Target]; !ok {
				err := lc.DeleteDomainRecord(ctx, options.DomainID, r.ID)
				if err != nil {
					return err
				}
			} else {
				existv4[r.Target] = struct{}{}
			}
		}
		if r.Type == linodego.RecordTypeAAAA && r.Name == "" {
			if _, ok := goalv6[r.Target]; !ok {
				err := lc.DeleteDomainRecord(ctx, options.DomainID, r.ID)
				if err != nil {
					return err
				}
			} else {
				existv6[r.Target] = struct{}{}
			}
		}
	}

	for _, ip := range ipv4 {
		if _, ok := existv4[ip]; !ok {
			_, err := lc.CreateDomainRecord(ctx, options.DomainID, linodego.DomainRecordCreateOptions{
				Name:   "",
				Type:   linodego.RecordTypeA,
				Target: ip,
			})
			if err != nil {
				return err
			}
		}
	}
	for _, ip := range ipv6 {
		if _, ok := existv6[ip]; !ok {
			_, err := lc.CreateDomainRecord(ctx, options.DomainID, linodego.DomainRecordCreateOptions{
				Name:   "",
				Type:   linodego.RecordTypeAAAA,
				Target: ip,
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}
