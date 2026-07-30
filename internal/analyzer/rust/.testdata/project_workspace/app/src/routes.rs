use core::service::Service;
use core::model::Request;
use log::debug;

pub fn setup(svc: &Service) {
    let req = Request::new();
    debug!("setting up routes");
    svc.handle(&req);
}
