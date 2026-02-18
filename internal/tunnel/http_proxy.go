package tunnel

// HTTP reverse proxy for Minecraft web maps (Dynmap, BlueMap, etc.).
// Listens on port 80, reads the HTTP Host header, extracts the subdomain,
// and routes to the correct tunnel client.
//
// Expected Host header format: map.happy-cat.eu.domain.com
// The subdomain "happy-cat" is the tunnel identifier.

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

func (s *Server) startHTTPProxy(ctx context.Context) {
	addr := fmt.Sprintf("0.0.0.0:%d", s.httpProxyPort)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("[HTTPProxy] Failed to listen on %s: %v", addr, err)
		return
	}
	log.Printf("[HTTPProxy] HTTP proxy listening on :%d (shared, routed by Host header)", s.httpProxyPort)

	go func() {
		<-ctx.Done()
		l.Close()
	}()

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					time.Sleep(50 * time.Millisecond)
					continue
				}
			}
			go s.handleHTTPConnection(conn)
		}
	}()
}

func (s *Server) handleHTTPConnection(clientConn net.Conn) {
	defer clientConn.Close()

	clientConn.SetReadDeadline(time.Now().Add(10 * time.Second))

	// Buffer the entire request to relay it later
	raw := &bytes.Buffer{}
	reader := bufio.NewReader(clientConn)

	var hostHeader string

	// Read HTTP headers line by line
	for {
		line, err := reader.ReadString('\n')
		raw.WriteString(line)
		if err != nil {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			// End of headers
			break
		}
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "host:") {
			hostHeader = strings.TrimSpace(trimmed[5:])
		}
	}

	clientConn.SetReadDeadline(time.Time{})

	if hostHeader == "" {
		log.Printf("[HTTPProxy] No Host header in request")
		return
	}

	// Strip port from host header if present
	host := hostHeader
	if h, _, err := net.SplitHostPort(hostHeader); err == nil {
		host = h
	}

	// Extract subdomain from host
	// "map.happy-cat.eu.domain.com" → "happy-cat"
	subdomain := extractSubdomainFromAddr(host, s.domain)
	if subdomain == "" {
		log.Printf("[HTTPProxy] Could not extract subdomain from Host: %s", hostHeader)
		return
	}

	tunnelIDRaw, ok := s.subdomainMap.Load(subdomain)
	if !ok {
		log.Printf("[HTTPProxy] No tunnel for subdomain %q", subdomain)
		return
	}
	tunnelID := tunnelIDRaw.(string)

	// Check HTTP is enabled for this tunnel
	httpPortRaw, ok := s.tunnelHTTPPort.Load(tunnelID)
	if !ok {
		log.Printf("[HTTPProxy] HTTP not enabled for tunnel %s", tunnelID)
		return
	}
	httpPort := httpPortRaw.(int)

	clientRaw, ok := s.clients.Load(tunnelID)
	if !ok {
		log.Printf("[HTTPProxy] No client connected for tunnel %s", tunnelID)
		return
	}
	client := clientRaw.(*ClientConn)

	connID := generateID()
	dataCh := make(chan net.Conn, 1)
	client.pendingTCP.Store(connID, dataCh)
	defer client.pendingTCP.Delete(connID)

	if err := client.send(fmt.Sprintf("OPEN %s %d", connID, httpPort)); err != nil {
		log.Printf("[HTTPProxy] Failed to send OPEN: %v", err)
		return
	}

	select {
	case dataConn := <-dataCh:
		defer dataConn.Close()
		// Send buffered headers first, then relay remaining request body and response
		dataConn.Write(raw.Bytes())
		// Also relay any remaining buffered bytes from the reader
		if reader.Buffered() > 0 {
			buf := make([]byte, reader.Buffered())
			reader.Read(buf)
			dataConn.Write(buf)
		}
		relay(clientConn, dataConn)
	case <-time.After(dataConnTimeout):
		log.Printf("[HTTPProxy] Timeout waiting for data conn (tunnel %s)", tunnelID)
	}
}
