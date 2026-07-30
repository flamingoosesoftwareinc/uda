use crate::model::Request;
use std::fmt;

pub struct Service;

impl Service {
    pub fn new() -> Self {
        Service
    }

    pub fn handle(&self, req: &Request) {
        let msg = fmt::format(format_args!("handling: {}", req.body));
        println!("{}", msg);
    }
}
