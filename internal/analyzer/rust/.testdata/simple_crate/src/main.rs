use std::fmt;

fn main() {
    let msg = fmt::format(format_args!("hello, world!"));
    println!("{}", msg);
}
