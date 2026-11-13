package stream

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"streamsonic/internal/config"
)

func OpenSource(ctx context.Context, in config.Input) (io.ReadCloser, error) {
	kind := in.Type
	if kind == "" {
		kind = config.TypeOfURL(in.URL)
	}
	switch kind {
	case "tshttp", "mpegts":
		return openHTTP(ctx, in)
	case "udp", "rtp":
		return openUDP(ctx, in)
	case "file":
		return openFile(in)
	case "hls", "hlss":
		return openHLS(ctx, in)
	}
	return nil, fmt.Errorf("input protocol %q is not implemented", kind)
}

func openHTTP(ctx context.Context, in config.Input) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.URL, nil)
	if err != nil {
		return nil, err
	}
	if in.UserAgent != "" {
		req.Header.Set("User-Agent", in.UserAgent)
	} else {
		req.Header.Set("User-Agent", "streamsonic/1.0")
	}
	for k, v := range in.Headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("input returned HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func openUDP(ctx context.Context, in config.Input) (io.ReadCloser, error) {
	u, err := url.Parse(in.URL)
	if err != nil {
		return nil, err
	}
	addr := u.Host
	if u.Opaque != "" {
		addr = u.Opaque
	}
	if !strings.Contains(addr, ":") {
		addr += ":1234"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var conn net.PacketConn
	ip := net.ParseIP(host)
	if ip != nil && ip.IsMulticast() {
		conn, err = net.ListenMulticastUDP("udp", nil, &net.UDPAddr{IP: ip, Port: portOf(addr)})
	} else {
		conn, err = net.ListenPacket("udp", addr)
	}
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	go func() {
		defer conn.Close()
		buf := make([]byte, 65536)
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				pw.CloseWithError(err)
				return
			}
			if _, err := pw.Write(buf[:n]); err != nil {
				return
			}
			if ctx.Err() != nil {
				pw.Close()
				return
			}
		}
	}()
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	return pr, nil
}

func portOf(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 1234
	}
	v, _ := strconv.Atoi(p)
	return v
}

func openFile(in config.Input) (io.ReadCloser, error) {
	path := in.URL
	if strings.HasPrefix(path, "file://") {
		path = strings.TrimPrefix(path, "file://")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func openHLS(ctx context.Context, in config.Input) (io.ReadCloser, error) {
	pr, pw := io.Pipe()
	go func() {
		if err := hlsPump(ctx, in, pw); err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.Close()
	}()
	go func() {
		<-ctx.Done()
		pw.Close()
	}()
	return pr, nil
}

func hlsPump(ctx context.Context, in config.Input, w io.Writer) error {
	client := &http.Client{Timeout: 20 * time.Second}
	playlist := in.URL
	seen := map[string]bool{}
	first := true
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, playlist, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		lines := parseM3U8(string(body))
		if first {
			for _, l := range lines {
				if !strings.HasPrefix(l, "#") && !strings.Contains(l, "://") && strings.HasSuffix(l, ".m3u8") {
					playlist = resolveURL(playlist, l)
					first = false
					break
				}
			}
			if first {
				first = false
			}
			continue
		}
		for _, l := range lines {
			if strings.HasPrefix(l, "#") || l == "" {
				continue
			}
			segURL := resolveURL(playlist, l)
			if seen[segURL] {
				continue
			}
			seen[segURL] = true
			if len(seen) > 4000 {
				seen = map[string]bool{segURL: true}
			}
			if err := fetchInto(ctx, client, segURL, w); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return ctx.Err()
}

func fetchInto(ctx context.Context, client *http.Client, u string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(w, resp.Body)
	return err
}

func parseM3U8(body string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		out = append(out, strings.TrimSpace(sc.Text()))
	}
	return out
}

func resolveURL(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}
