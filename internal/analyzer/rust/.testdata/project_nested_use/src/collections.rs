pub struct HashMap<K, V> {
    _k: std::marker::PhantomData<K>,
    _v: std::marker::PhantomData<V>,
}

impl<K, V> HashMap<K, V> {
    pub fn new() -> Self {
        Self {
            _k: std::marker::PhantomData,
            _v: std::marker::PhantomData,
        }
    }
}

pub struct HashSet<T> {
    _t: std::marker::PhantomData<T>,
}

impl<T> HashSet<T> {
    pub fn new() -> Self {
        Self {
            _t: std::marker::PhantomData,
        }
    }
}

pub mod hash_map {
    pub struct Entry;
    pub struct OccupiedEntry;
    pub struct VacantEntry;
}
