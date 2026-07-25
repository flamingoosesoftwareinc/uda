mod routes;

use core::service::Service;
use log::info;

fn main() {
    let svc = Service::new();
    info!("starting");
    routes::setup(&svc);
}
