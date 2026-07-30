use serde::Deserialize;

#[derive(Deserialize)]
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
