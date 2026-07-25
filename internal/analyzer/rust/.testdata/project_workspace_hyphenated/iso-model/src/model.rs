use casesplit::tokens::Token;

pub struct Mirror {
    pub token: Token,
}

impl Mirror {
    pub fn new() -> Self {
        Mirror { token: Token::new() }
    }
}
