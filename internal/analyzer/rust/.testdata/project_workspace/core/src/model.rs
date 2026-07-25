use serde::Serialize;

#[derive(Serialize)]
pub struct Request {
    pub body: String,
}

impl Request {
    pub fn new() -> Self {
        Request {
            body: String::new(),
        }
    }
}
