// Simple nested use
use crate::collections::{HashMap, HashSet};

// Deeply nested use
use crate::collections::{hash_map::{Entry, OccupiedEntry}};

// Multiple items at different nesting levels
use crate::io::{Read, Write, prelude};

fn main() {
    // Use the imports
    let _map: HashMap<String, i32> = HashMap::new();
    let _set: HashSet<String> = HashSet::new();
    
    let _entry: Entry = Entry;
    let _occupied: OccupiedEntry = OccupiedEntry;
    
    let _read: Read = Read;
    let _write: Write = Write;
}
