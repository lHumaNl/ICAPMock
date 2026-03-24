// Package server provides the ICAP server implementation for handling
// ICAP (Internet Content Adaptation Protocol) requests per RFC 3507.
//
// The server package provides:
//   - ICAPServer: A full-featured ICAP server with graceful shutdown
//   - Connection management with connection pooling and limiting
//   - Protocol parsing for ICAP requests and responses
//   - Full streaming support for large bodies (O(1) memory)
//   - TLS support for secure connections
//
// # Architecture
//
// The server architecture consists of three main components:
//
//  1. ICAPServer: The main server that accepts connections and routes requests
//  2. Connection: Wraps net.Conn with buffered I/O and timeout management
//  3. ConnectionPool: Manages active connections for graceful shutdown
//
// # Usage Example
//
// Basic server setup:
//
//	cfg := &config.ServerConfig{
//	    Host:           "0.0.0.0",
//	    Port:           1344,
//	    ReadTimeout:    30 * time.Second,
//	    WriteTimeout:   30 * time.Second,
//	    MaxConnections: 1000,
//	    Streaming:      true,
//	}
//
//	srv, err := server.NewServer(cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Set up router
//	r := router.NewRouter()
//	r.Handle("/reqmod", &ReqmodHandler{})
//	r.Handle("/respmod", &RespmodHandler{})
//	srv.SetRouter(r)
//
//	// Start server
//	ctx := context.Background()
//	if err := srv.Start(ctx); err != nil {
//	    log.Fatal(err)
//	}
//	defer srv.Stop(ctx)
//
// # Graceful Shutdown
//
// The server supports graceful shutdown. When Stop() is called:
//  1. The server stops accepting new connections
//  2. All active connections are allowed to complete
//  3. Stop() blocks until all connections are finished
//
// There is no timeout on graceful shutdown - the server will wait indefinitely
// for all active requests to complete. This ensures no requests are dropped.
//
// # Connection Limiting
//
// The server uses a semaphore to limit concurrent connections. When the limit
// is reached, new connections are immediately rejected. This prevents resource
// exhaustion under high load.
//
// # Streaming
//
// When Streaming is enabled in the configuration, the server uses O(1) memory
// for body handling. Large request/response bodies are streamed directly from
// the network without buffering the entire body in memory.
//
// # TLS Support
//
// To enable TLS, configure the TLS section in ServerConfig:
//
//	cfg := &config.ServerConfig{
//	    // ...
//	    TLS: config.TLSConfig{
//	        Enabled:  true,
//	        CertFile: "/path/to/cert.pem",
//	        KeyFile:  "/path/to/key.pem",
//	    },
//	}
package server
