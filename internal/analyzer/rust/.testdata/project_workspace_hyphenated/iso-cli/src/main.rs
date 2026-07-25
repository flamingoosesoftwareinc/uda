use casesplit::tokens::Token;
use iso_model::model::Mirror;
use renamed::report::Report;
use serde::Deserialize;

#[derive(Deserialize)]
struct Config {
    name: String,
}

fn main() {
    let mirror = Mirror::new();
    let token = Token::new();
    let report = Report::new();
}
