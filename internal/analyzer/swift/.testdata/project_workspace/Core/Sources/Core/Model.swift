import Foundation

public struct Request: Codable {
    public let path: String
    public let method: String

    public init(path: String, method: String) {
        self.path = path
        self.method = method
    }
}
