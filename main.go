package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/miekg/dns"
)

const (
	defaultListenAddr = ":53"
	defaultTTL        = 30
)

type resolver struct {
	cli         *client.Client
	domain      string
	network     string
	ttl         uint32
	disableIPv6 bool
}

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)

	domain := strings.TrimSuffix(strings.TrimSpace(getEnv("DOMAIN", "domain.local")), ".")
	listenAddr := getEnv("LISTEN_ADDR", defaultListenAddr)
	networkName := strings.TrimSpace(os.Getenv("DOCKER_NETWORK"))
	ttl := parseTTL(getEnv("TTL", strconv.Itoa(defaultTTL)))
	disableIPv6 := strings.EqualFold(strings.TrimSpace(os.Getenv("DISABLE_IPV6")), "true")

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Fatalf("create docker client: %v", err)
	}
	defer cli.Close()

	r := &resolver{
		cli:         cli,
		domain:      strings.ToLower(domain),
		network:     networkName,
		ttl:         ttl,
		disableIPv6: disableIPv6,
	}

	dns.HandleFunc(".", r.handleDNS)

	udpServer := &dns.Server{Addr: listenAddr, Net: "udp"}
	tcpServer := &dns.Server{Addr: listenAddr, Net: "tcp"}

	go func() {
		logger.Printf("starting UDP DNS server on %s for domain %s", listenAddr, r.domain)
		if err := udpServer.ListenAndServe(); err != nil && !isShutdownError(err) {
			logger.Fatalf("udp listen: %v", err)
		}
	}()

	go func() {
		logger.Printf("starting TCP DNS server on %s for domain %s", listenAddr, r.domain)
		if err := tcpServer.ListenAndServe(); err != nil && !isShutdownError(err) {
			logger.Fatalf("tcp listen: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = udpServer.ShutdownContext(shutdownCtx)
	_ = tcpServer.ShutdownContext(shutdownCtx)
}

func (r *resolver) handleDNS(w dns.ResponseWriter, req *dns.Msg) {
	msg := new(dns.Msg)
	msg.SetReply(req)
	msg.Authoritative = true

	for _, q := range req.Question {
		if q.Qclass != dns.ClassINET {
			continue
		}

		containerName, ok := matchContainerName(q.Name, r.domain)
		if !ok {
			msg.Rcode = dns.RcodeNameError
			continue
		}

		answers, err := r.lookup(q, containerName)
		if err != nil {
			msg.Rcode = dns.RcodeServerFailure
			log.Printf("lookup %s failed: %v", containerName, err)
			continue
		}
		if len(answers) == 0 {
			msg.Rcode = dns.RcodeNameError
			continue
		}

		msg.Answer = append(msg.Answer, answers...)
	}

	if len(msg.Answer) == 0 && msg.Rcode == dns.RcodeSuccess && len(req.Question) > 0 {
		msg.Rcode = dns.RcodeNameError
	}

	_ = w.WriteMsg(msg)
}

func (r *resolver) lookup(q dns.Question, containerName string) ([]dns.RR, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	inspect, err := r.cli.ContainerInspect(ctx, containerName)
	if err != nil {
		if client.IsErrNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	targets := selectIPs(inspect, r.network, r.disableIPv6)
	if len(targets) == 0 {
		return nil, nil
	}

	answers := make([]dns.RR, 0, len(targets))
	for _, ip := range targets {
		if q.Qtype == dns.TypeA && ip.To4() != nil {
			answers = append(answers, &dns.A{
				Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: r.ttl},
				A:   ip,
			})
		}
		if q.Qtype == dns.TypeAAAA && ip.To4() == nil {
			answers = append(answers, &dns.AAAA{
				Hdr:  dns.RR_Header{Name: q.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: r.ttl},
				AAAA: ip,
			})
		}
	}

	return answers, nil
}

func selectIPs(inspect container.InspectResponse, preferredNetwork string, disableIPv6 bool) []net.IP {
	networks := inspect.NetworkSettings.Networks
	if len(networks) == 0 {
		return nil
	}

	if preferredNetwork != "" {
		if settings, ok := networks[preferredNetwork]; ok {
			return settingsToIPs(settings, disableIPv6)
		}
	}

	names := make([]string, 0, len(networks))
	for name := range networks {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		ips := settingsToIPs(networks[name], disableIPv6)
		if len(ips) > 0 {
			return ips
		}
	}

	return nil
}

func settingsToIPs(settings *network.EndpointSettings, disableIPv6 bool) []net.IP {
	if settings == nil {
		return nil
	}

	var ips []net.IP
	if ip := net.ParseIP(settings.IPAddress); ip != nil {
		ips = append(ips, ip)
	}
	if !disableIPv6 {
		if ip := net.ParseIP(settings.GlobalIPv6Address); ip != nil {
			ips = append(ips, ip)
		}
	}
	return ips
}

func isShutdownError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "server shutdown")
}

func matchContainerName(qName, domain string) (string, bool) {
	name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(qName)), ".")
	suffix := "." + strings.ToLower(strings.TrimSpace(domain))
	if !strings.HasSuffix(name, suffix) {
		return "", false
	}

	containerName := strings.TrimSuffix(name, suffix)
	if containerName == "" || strings.Contains(containerName, ".") {
		return "", false
	}

	return containerName, true
}

func getEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func parseTTL(value string) uint32 {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return defaultTTL
	}
	return uint32(parsed)
}
