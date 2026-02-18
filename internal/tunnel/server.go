package tunnel

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Protocol messages (newline-terminated plain text)
// Control channel (client → server):
//
//	AUTH <jwt_token> <tunnel_id>
//	PONG
//	UDP_REPLY <conn_id> <hex_payload>
//
// Control channel (server → client):
//
//	OK
//	ERROR <message>
//	OPEN <conn_id> <local_port>      (new TCP connection arrived, open data channel)
//	UDP_PKT <conn_id> <local_port> <hex_payload>  (UDP packet arrived)
//	PING
//
// Data channel (client → server, first message only):
//
//	DATA <conn_id>
//
// After server pairs it, raw bytes flow bidirectionally.
const (
	controlTimeout  = 10 * time.Second
	pingInterval    = 30 * time.Second
	dataConnTimeout = 15 * time.Second
)

// TunnelRegistration holds the parameters to register a tunnel with the server.
type TunnelRegistration struct {
	TunnelID      string
	Subdomain     string
	MCLocalPort   int
	HTTPLocalPort *int // nil = disabled
	UDPLocalPort  int
	UDPPublicPort *int // nil = no dedicated UDP port
}

// Server is the core tunnel server.
// Minecraft TCP is proxied via startMCProxy (shared port, routed by MC handshake).
// HTTP is proxied via startHTTPProxy (shared port, routed by Host header).
// Voice chat UDP gets one dedicated public port per tunnel from the pool.
type Server struct {
	jwtSecret     []byte
	tunnelPort    int
	mcProxyPort   int
	httpProxyPort int
	domain        string
	minPort       int
	maxPort       int

	// tunnelID → *ClientConn (currently connected clients)
	clients sync.Map

	// subdomain → tunnelID (registered/active tunnels)
	subdomainMap sync.Map

	// tunnelID → mc_local_port
	tunnelMCPort sync.Map

	// tunnelID → http_local_port (only set when HTTP is enabled)
	tunnelHTTPPort sync.Map

	// UDP voice chat: public_port → tunnelID
	portOwners sync.Map

	// UDP voice chat: public_port → local_port
	portLocalMap sync.Map

	// UDP voice chat: public_port → net.PacketConn (active listeners)
	udpListeners sync.Map

	// UDP voice chat: playerAddr → *udpPlayerEntry (persistent, for routing UDP_REPLY back to player)
	udpPlayerMap sync.Map
}

type udpPlayerEntry struct {
	pc   net.PacketConn
	addr net.Addr
}

func NewServer(jwtSecret []byte, tunnelPort, mcProxyPort, httpProxyPort int, domain string, minPort, maxPort int) *Server {
	return &Server{
		jwtSecret:     jwtSecret,
		tunnelPort:    tunnelPort,
		mcProxyPort:   mcProxyPort,
		httpProxyPort: httpProxyPort,
		domain:        domain,
		minPort:       minPort,
		maxPort:       maxPort,
	}
}

// RegisterTunnel activates a tunnel: registers subdomain routing and starts UDP listener if needed.
// Called when a tunnel is started via the API (or restored on server startup).
func (s *Server) RegisterTunnel(reg TunnelRegistration) {
	s.subdomainMap.Store(reg.Subdomain, reg.TunnelID)
	s.tunnelMCPort.Store(reg.TunnelID, reg.MCLocalPort)

	if reg.HTTPLocalPort != nil {
		s.tunnelHTTPPort.Store(reg.TunnelID, *reg.HTTPLocalPort)
	} else {
		s.tunnelHTTPPort.Delete(reg.TunnelID)
	}

	if reg.UDPPublicPort != nil {
		// Only start listener if not already running
		if _, running := s.udpListeners.Load(*reg.UDPPublicPort); !running {
			s.portOwners.Store(*reg.UDPPublicPort, reg.TunnelID)
			s.portLocalMap.Store(*reg.UDPPublicPort, reg.UDPLocalPort)
			go s.startUDPPortListener(*reg.UDPPublicPort, reg.TunnelID, reg.UDPLocalPort)
		}
	}
}

// UnregisterTunnel deactivates a tunnel: removes subdomain routing and stops UDP listener.
// Called when a tunnel is stopped via the API.
func (s *Server) UnregisterTunnel(tunnelID, subdomain string, udpPublicPort *int) {
	s.subdomainMap.Delete(subdomain)
	s.tunnelMCPort.Delete(tunnelID)
	s.tunnelHTTPPort.Delete(tunnelID)

	if udpPublicPort != nil {
		s.portOwners.Delete(*udpPublicPort)
		s.portLocalMap.Delete(*udpPublicPort)
		if pc, ok := s.udpListeners.LoadAndDelete(*udpPublicPort); ok {
			pc.(net.PacketConn).Close()
		}
	}

	// Disconnect client if still connected
	if c, ok := s.clients.LoadAndDelete(tunnelID); ok {
		c.(*ClientConn).close()
	}
}

// IsClientConnected returns true if a VoidLink desktop client is connected for this tunnel.
func (s *Server) IsClientConnected(tunnelID string) bool {
	_, ok := s.clients.Load(tunnelID)
	return ok
}

// IsUDPPortInUse returns true if the given public port is already allocated.
func (s *Server) IsUDPPortInUse(port int) bool {
	_, ok := s.portOwners.Load(port)
	return ok
}

// Run starts the control server and the shared MC/HTTP proxies.
func (s *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", s.tunnelPort))
	if err != nil {
		return fmt.Errorf("failed to listen on tunnel port %d: %w", s.tunnelPort, err)
	}

	log.Printf("[Tunnel] Control server running on :%d", s.tunnelPort)

	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					log.Printf("[Tunnel] Accept error: %v", err)
					time.Sleep(100 * time.Millisecond)
					continue
				}
			}
			go s.handleNewConn(conn)
		}
	}()

	// Start shared TCP proxies
	s.startMCProxy(ctx)
	s.startHTTPProxy(ctx)

	return nil
}

// ---- Control Connection Handler ----

func (s *Server) handleNewConn(conn net.Conn) {
	conn.SetDeadline(time.Now().Add(controlTimeout))
	reader := bufio.NewReader(conn)

	line, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return
	}
	line = strings.TrimSpace(line)
	parts := strings.Fields(line)
	if len(parts) == 0 {
		conn.Close()
		return
	}

	conn.SetDeadline(time.Time{})

	switch parts[0] {
	case "AUTH":
		if len(parts) < 3 {
			conn.Write([]byte("ERROR invalid handshake\n"))
			conn.Close()
			return
		}
		s.handleControlConnFromReader(conn, bufio.NewReaderSize(reader, 4096), parts[1], parts[2])
	case "DATA":
		if len(parts) < 2 {
			conn.Close()
			return
		}
		connID := parts[1]
		conn.Write([]byte("OK\n"))
		s.handleDataConn(conn, connID)
	default:
		conn.Write([]byte("ERROR unknown command\n"))
		conn.Close()
	}
}

func (s *Server) handleControlConnFromReader(conn net.Conn, reader *bufio.Reader, tokenStr, tunnelID string) {
	if err := s.validateJWT(tokenStr); err != nil {
		conn.Write([]byte("ERROR unauthorized\n"))
		conn.Close()
		log.Printf("[Tunnel] Auth failed for tunnel %s: %v", tunnelID, err)
		return
	}

	// Check that this tunnel is registered (subdomain must be in the map)
	isRegistered := false
	s.subdomainMap.Range(func(_, v any) bool {
		if v.(string) == tunnelID {
			isRegistered = true
			return false
		}
		return true
	})
	if !isRegistered {
		conn.Write([]byte("ERROR tunnel not active\n"))
		conn.Close()
		log.Printf("[Tunnel] Client attempted connection for unregistered tunnel %s", tunnelID)
		return
	}

	client := &ClientConn{
		tunnelID: tunnelID,
		conn:     conn,
		reader:   reader,
		writer:   bufio.NewWriter(conn),
	}

	if old, ok := s.clients.LoadAndDelete(tunnelID); ok {
		old.(*ClientConn).close()
	}
	s.clients.Store(tunnelID, client)

	conn.Write([]byte("OK\n"))
	log.Printf("[Tunnel] Client connected for tunnel %s", tunnelID)

	go s.pingLoop(client)
	s.readControlLoop(client)

	s.clients.CompareAndDelete(tunnelID, client)
	log.Printf("[Tunnel] Client disconnected for tunnel %s", tunnelID)
}

func (s *Server) readControlLoop(client *ClientConn) {
	for {
		client.conn.SetReadDeadline(time.Now().Add(pingInterval * 2))
		line, err := client.reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		switch parts[0] {
		case "PONG":
			// keepalive received
		case "UDP_REPLY":
			if len(parts) < 3 {
				continue
			}
			connID := parts[1]
			data, err := hex.DecodeString(parts[2])
			if err != nil {
				continue
			}
			// Route reply back to player using persistent session map
			if entryRaw, ok := s.udpPlayerMap.Load(connID); ok {
				entry := entryRaw.(*udpPlayerEntry)
				_, _ = entry.pc.WriteTo(data, entry.addr)
			}
		}
	}
}

func (s *Server) pingLoop(client *ClientConn) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for range ticker.C {
		if err := client.send("PING"); err != nil {
			return
		}
	}
}

// ---- UDP Port Listener (voice chat) ----

func (s *Server) startUDPPortListener(publicPort int, tunnelID string, localPort int) {
	addr := fmt.Sprintf("0.0.0.0:%d", publicPort)
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		log.Printf("[Tunnel] Failed to listen on UDP %s: %v", addr, err)
		return
	}
	s.udpListeners.Store(publicPort, pc)
	log.Printf("[Tunnel] UDP voice chat on :%d → tunnel %s (local:%d)", publicPort, tunnelID, localPort)

	buf := make([]byte, 65535)
	for {
		n, remoteAddr, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		data := make([]byte, n)
		copy(data, buf[:n])
		go s.handleUDPPacket(pc, remoteAddr, data, tunnelID, localPort)
	}
}

func (s *Server) handleUDPPacket(pc net.PacketConn, addr net.Addr, data []byte, tunnelID string, localPort int) {
	clientRaw, ok := s.clients.Load(tunnelID)
	if !ok {
		return
	}
	client := clientRaw.(*ClientConn)

	connID := addr.String()

	// Register/refresh persistent player entry so UDP_REPLY can find the right PacketConn+addr
	s.udpPlayerMap.Store(connID, &udpPlayerEntry{pc: pc, addr: addr})

	hexData := hex.EncodeToString(data)
	_ = client.send(fmt.Sprintf("UDP_PKT %s %d %s", connID, localPort, hexData))
}

// ---- Data Connection Handler ----

func (s *Server) handleDataConn(conn net.Conn, connID string) {
	var found *ClientConn
	s.clients.Range(func(_, v any) bool {
		c := v.(*ClientConn)
		if _, ok := c.pendingTCP.Load(connID); ok {
			found = c
			return false
		}
		return true
	})

	if found == nil {
		log.Printf("[Tunnel] No pending connection for DATA %s", connID)
		conn.Close()
		return
	}

	if ch, ok := found.pendingTCP.Load(connID); ok {
		select {
		case ch.(chan net.Conn) <- conn:
		default:
			conn.Close()
		}
	}
}

// ---- JWT validation ----

func (s *Server) validateJWT(tokenStr string) error {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return err
	}
	if !token.Valid {
		return fmt.Errorf("invalid token")
	}
	return nil
}

// ---- ClientConn ----

type ClientConn struct {
	tunnelID   string
	conn       net.Conn
	reader     *bufio.Reader
	writer     *bufio.Writer
	mu         sync.Mutex
	pendingTCP sync.Map // connID → chan net.Conn
}

func (c *ClientConn) send(msg string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.writer.WriteString(msg + "\n")
	if err != nil {
		return err
	}
	return c.writer.Flush()
}

func (c *ClientConn) close() {
	c.conn.Close()
}

// ---- Helpers ----

func relay(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		buf := make([]byte, 32*1024)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				dst.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	a.Close()
	b.Close()
}

func generateID() string {
	b := make([]byte, 8)
	ts := time.Now().UnixNano()
	for i := 0; i < 8; i++ {
		b[i] = byte(ts >> (i * 8))
	}
	return hex.EncodeToString(b)
}
