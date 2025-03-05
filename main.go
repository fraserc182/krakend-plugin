package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/luraproject/lura/v2/logging"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
)

// Plugin implements the KrakenD plugin interface
func main() {}

// Plugin is the exported symbol KrakenD will look for
var Plugin = func(
	ctx context.Context,
	extra map[string]interface{},
	logger logging.Logger,
) (http.Handler, error) {
	logger.Debug("GTFS-RT to JSON plugin loaded")
	return &gtfsrtPlugin{logger: logger}, nil
}

// gtfsrtPlugin is the plugin implementation
type gtfsrtPlugin struct {
	logger logging.Logger
}

// ServeHTTP handles the HTTP request
func (g *gtfsrtPlugin) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	g.logger.Debug("Processing request:", req.URL.String())

	// Create client for the backend request
	client := &http.Client{}

	// Copy the original request
	backendReq, err := http.NewRequest(req.Method, req.URL.String(), req.Body)
	if err != nil {
		g.logger.Error("Error creating backend request:", err.Error())
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Copy headers
	for name, values := range req.Header {
		for _, value := range values {
			backendReq.Header.Add(name, value)
		}
	}

	// Send the request to the backend
	resp, err := client.Do(backendReq)
	if err != nil {
		g.logger.Error("Error sending request to backend:", err.Error())
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		g.logger.Error("Error reading response body:", err.Error())
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Check if this looks like GTFS-RT data
	contentType := resp.Header.Get("Content-Type")
	isGTFSRT := contentType == "application/x-protobuf" ||
		contentType == "application/protobuf" ||
		strings.Contains(contentType, "octet-stream") ||
		strings.Contains(req.URL.Path, "gtfs") ||
		strings.Contains(req.URL.Path, "tripupdates")

	if !isGTFSRT {
		g.logger.Debug("Not GTFS-RT data, passing through original response")
		// Just pass through the original response
		for name, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	// Try to unmarshal as GTFS-RT
	feed := &gtfs.FeedMessage{}
	err = proto.Unmarshal(body, feed)
	if err != nil {
		g.logger.Error("Failed to unmarshal GTFS-RT data:", err.Error())
		g.logger.Debug("Response body start:", fmt.Sprintf("%x", body[:min(50, len(body))]))
		// Return original data on error
		for name, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	// Use the standard protojson converter
	marshaler := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
		Indent:          "  ",
	}

	jsonData, err := marshaler.Marshal(feed)
	if err != nil {
		g.logger.Error("Failed to convert to JSON:", err.Error())
		// Return original data on error
		for name, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	// Copy essential headers
	for name, values := range resp.Header {
		if name != "Content-Type" && name != "Content-Length" {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
	}

	// Set JSON content type and send response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(jsonData)
	g.logger.Debug("Successfully converted GTFS-RT to JSON")
}

// Helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Init function for plugin registration
func init() {
	fmt.Println("GTFS-RT to JSON plugin registered")
}
