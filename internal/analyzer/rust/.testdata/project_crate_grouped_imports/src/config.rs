use serde::{Serialize, Deserialize};

#[derive(Serialize, Deserialize)]
pub struct Config {
    pub name: String,
}

impl Config {
    pub fn new() -> Self {
        Config {
            name: String::from("default"),
        }
    }
}
