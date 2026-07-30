use crate::config::Config;
use std::fmt;

pub fn handle(cfg: &Config) {
    let msg = fmt::format(format_args!("handling: {}", cfg.name));
    println!("{}", msg);
}
