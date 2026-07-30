pub mod router;
mod middleware;

use super::config::Config;
use self::middleware::Logger;

pub fn init(_cfg: &Config) {
    let _logger = Logger::new();
}
