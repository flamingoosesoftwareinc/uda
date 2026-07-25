mod config;
mod handler;

use crate::config::Config;
use crate::handler::handle;

fn main() {
    let cfg = Config::new();
    handle(&cfg);
}
