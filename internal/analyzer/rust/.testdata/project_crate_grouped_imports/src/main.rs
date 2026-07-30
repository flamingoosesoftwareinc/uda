mod config;
mod handler;

use crate::config::Config;
use crate::handler::router::Router;
use std::collections::{HashMap, HashSet};
use tokio::runtime::Runtime as TokioRuntime;

fn main() {
    let _map: HashMap<String, String> = HashMap::new();
    let _set: HashSet<String> = HashSet::new();
    let _cfg = Config::new();
    let _rt = TokioRuntime::new().unwrap();
    let _router = Router::new();
}
