import Foundation

struct Cluster: Codable, Identifiable {
    let id: String
    let name: String
    let description: String
    let status: String
    let headNodeId: String
    let workerIds: [String]
    let dashboardUrl: String
    let rayPort: Int
    let dashboardPort: Int
    let createdAt: Date
    let updatedAt: Date

    var workerCount: Int { workerIds.count }
    var isRunning: Bool { status == "running" }
}

struct ClusterHealth: Codable {
    let clusterId: String
    let name: String
    let status: String
    let nodes: [NodeStatus]

    struct NodeStatus: Codable, Identifiable {
        let nodeId: String
        let role: String
        let healthy: Bool
        let error: String?
        var id: String { nodeId }
    }
}

struct CreateClusterRequest: Encodable {
    let name: String
    let head_id: String
    let worker_ids: [String]
}
