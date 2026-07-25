use core::models::User;
use db::connection::Pool;

fn main() {
    let _user = User { name: String::from("alice") };
    let _pool = Pool::new();
}
