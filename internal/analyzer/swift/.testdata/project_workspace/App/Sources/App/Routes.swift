import Foundation
import Core

func setupRoutes(service: APIService) {
    let request = Request(path: "/api/health", method: "GET")
    _ = service.handleRequest(request)
}
