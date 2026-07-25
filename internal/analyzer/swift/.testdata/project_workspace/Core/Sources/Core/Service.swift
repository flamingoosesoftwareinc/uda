import Foundation

public class APIService {
    public init() {}

    public func handleRequest(_ request: Request) -> String {
        return "Handled: \(request.path)"
    }
}
