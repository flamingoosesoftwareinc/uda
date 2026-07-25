use crate::config::Config;
use std::io::{self, Read, Write};

pub struct Router;

impl Router {
    pub fn new() -> Self {
        Router
    }

    pub fn handle(&self, _cfg: &Config) -> io::Result<()> {
        Ok(())
    }
}
