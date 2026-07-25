use serde::Serialize;

#[derive(Serialize)]
pub struct Logger {
    pub level: String,
}

impl Logger {
    pub fn new() -> Self {
        Logger {
            level: String::from("info"),
        }
    }
}
