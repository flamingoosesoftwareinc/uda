package domain

type Entity struct{ ID string }

func New(id string) Entity { return Entity{ID: id} }
