import Foundation

public class UserService {
    public init() {}

    public func fetchUser() -> User {
        return User(id: UUID(), name: "John")
    }
}
