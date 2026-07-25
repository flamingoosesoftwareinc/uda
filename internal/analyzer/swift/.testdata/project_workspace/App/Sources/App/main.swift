import Foundation
import Core

let service = APIService()
let request = Request(path: "/api/users", method: "GET")
let response = service.handleRequest(request)
print(response)
