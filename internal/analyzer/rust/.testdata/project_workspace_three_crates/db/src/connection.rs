use core::models::User;

pub struct Pool;

impl Pool {
    pub fn new() -> Self {
        Pool
    }

    pub fn get_user(&self) -> User {
        User { name: String::from("db_user") }
    }
}
