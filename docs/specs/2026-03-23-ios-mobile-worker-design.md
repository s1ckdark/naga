# iOS Mobile Worker App - Capability-First Architecture

## Overview
Extend clusterManager with iOS mobile worker support. All nodes (server/mobile) use unified capability registration. iOS app serves as monitor, worker (GPS/camera/SMS/sensors), and head orchestrator.

## Architecture
- **Capability Registry**: Device model gets `capabilities []string`, all nodes register capabilities
- **Task Queue**: Server-side task creation, capability matching, distribution, status tracking
- **WebSocket Hub**: Real-time bidirectional communication for connected workers
- **APNS Gateway**: Wake background/terminated apps for task delivery
- **API Key Auth**: Fallback auth for non-Tailscale networks
- **Plugin System**: iOS capabilities as toggleable plugins
- **Offline Queue**: Local result storage, sync on reconnect

## Server Changes (Go)
1. Domain: Add `Capabilities`, `DeviceToken`, `ConnectionType` to Device; new `Task` model
2. WebSocket hub at `/ws`
3. Task queue + capability matcher
4. API endpoints: capability registration, task CRUD, WebSocket upgrade
5. API key auth middleware

## iOS Changes (Swift)
1. Add iOS target to existing Package.swift (multiplatform)
2. Shared models/APIClient between macOS and iOS
3. Capability plugin protocol + implementations (GPS, Camera, etc.)
4. WebSocket client (URLSessionWebSocketTask)
5. Offline queue (local JSON storage)
6. iOS UI: TabView (Dashboard, Devices, Tasks, Settings)

## Communication
- Foreground: WebSocket for task receipt
- Background: APNS push notification
- Results: WebSocket or REST fallback
- Monitoring: REST API (shared with macOS)

## Auth
- Tailscale network: automatic (IP-based)
- External: API key in Authorization header
