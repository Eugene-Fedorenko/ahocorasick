package cedar

import (
	"sync"
)

// Status reports the following statistics of the cedar:
//	keys:		number of keys that are in the cedar,
//	nodes:		number of trie nodes (slots in the base array) has been taken,
//	size:			the size of the base array used by the cedar,
//	capacity:		the capicity of the base array used by the cedar.
func (da *Cedar) Status() (keys, nodes, size, capacity int) {
	for i := 0; i < da.size; i++ {
		n := da.array[i]
		if n.Check >= 0 {
			nodes++
			if n.Value >= 0 {
				keys++
			}
		}
	}
	return keys, nodes, da.size, da.capacity
}

// Jump travels from a node `from` to another node `to` by following the path `path`.
// For example, if the following keys were inserted:
//	id	key
//	19	abc
//	23	ab
//	37	abcd
// then
//	Jump([]byte("ab"), 0) = 23, nil		// reach "ab" from root
//	Jump([]byte("c"), 23) = 19, nil			// reach "abc" from "ab"
//	Jump([]byte("cd"), 23) = 37, nil		// reach "abcd" from "ab"
func (da *Cedar) Jump(path []byte, from int) (to int, err error) {
	for _, b := range path {
		if da.array[from].Value >= 0 {
			return from, ErrNoPath
		}
		to = da.array[from].base() ^ int(b)
		if da.array[to].Check != from {
			return from, ErrNoPath
		}
		from = to
	}
	return to, nil
}

// Key returns the key of the node with the given `id`.
// It will return ErrNoPath, if the node does not exist.
func (da *Cedar) Key(id int) (key []byte, err error) {
	for id > 0 {
		from := da.array[id].Check
		if from < 0 {
			return nil, ErrNoPath
		}
		if char := byte(da.array[from].base() ^ id); char != 0 {
			key = append(key, char)
		}
		id = from
	}
	if id != 0 || len(key) == 0 {
		return nil, ErrInvalidKey
	}
	for i := 0; i < len(key)/2; i++ {
		key[i], key[len(key)-i-1] = key[len(key)-i-1], key[i]
	}
	return key, nil
}

// Value returns the value of the node with the given `id`.
// It will return ErrNoValue, if the node does not have a value.
func (da *Cedar) vKeyOf(id int) (value int, err error) {
	value = da.array[id].Value
	if value >= 0 {
		return value, nil
	}
	to := da.array[id].base()
	if da.array[to].Check == id && da.array[to].Value >= 0 {
		return da.array[to].Value, nil
	}
	return 0, ErrNoValue
}

// Insert adds a key-value pair into the cedar.
// It will return ErrInvalidValue, if value < 0 or >= valueLimit.
func (da *Cedar) Insert(key []byte, value interface{}) error {
	k := da.vKey()
	klen := len(key)
	p := da.get(key, 0, 0)
	//fmt.Printf("k:%s, v:%d\n", string(key), value)
	da.array[p].Value = k
	da.info[p].End = true
	da.vals[k] = nvalue{Len: klen, Value: value}
	return nil
}

// Update increases the value associated with the `key`.
// The `key` will be inserted if it is not in the cedar.
// It will return ErrInvalidValue, if the updated value < 0 or >= valueLimit.
func (da *Cedar) Update(key []byte, value int) error {
	id := da.get(key, 0, 0)
	p := &da.array[id].Value
	if *p+value < 0 || *p+value >= valueLimit {
		return ErrInvalidValue
	}
	*p += value
	return nil
}

// Delete removes a key-value pair from the cedar.
// It will return ErrNoPath, if the key has not been added.
func (da *Cedar) Delete(key []byte) (err error) {
	// if the path does not exist, or the end is not a leaf, nothing to delete
	to, err := da.Jump(key, 0)
	if err != nil {
		return err
	}
	vk, err := da.vKeyOf(to)
	if err != nil {
		return err
	}
	if _, ok := da.vals[vk]; !ok {
		return ErrNoValue
	}

	if da.array[to].Value < 0 {
		base := da.array[to].base()
		if da.array[base].Check == to {
			to = base
		}
	}

	for {
		from := da.array[to].Check
		base := da.array[from].base()
		label := byte(to ^ base)

		// if `to` has sibling, remove `to` from the sibling list, then stop
		if da.info[to].Sibling != 0 || da.info[from].Child != label {
			// delete the label from the child ring first
			da.popSibling(from, base, label)
			// then release the current node `to` to the empty node ring
			da.pushEnode(to)
			break
		}
		// otherwise, just release the current node `to` to the empty node ring
		da.pushEnode(to)
		// then check its parent node
		to = from
	}
	return
}

// Get returns the value associated with the given `key`.
// It is equivalent to
//		id, err1 = Jump(key)
//		value, err2 = Value(id)
// Thus, it may return ErrNoPath or ErrNoValue,
func (da *Cedar) Get(key []byte) (value interface{}, err error) {
	to, err := da.Jump(key, 0)
	if err != nil {
		return 0, err
	}
	vk, err := da.vKeyOf(to)
	if err != nil {
		return nil, ErrNoValue
	}
	if v, ok := da.vals[vk]; ok {
		return v.Value, nil
	}
	return nil, ErrNoValue
}

type snidpos struct {
	nid, pos int
}

var ssPool = sync.Pool{New: func() interface{} {
	a := make([]*snidpos, 0, 30)
	return &a
}}

var mPool = sync.Pool{New: func() interface{} {
	a := make(map[int]struct{})
	return &a
}}

func getsnidpos(a *[]*snidpos) *snidpos {
	l := len(*a)

	if l == cap(*a) {
		*a = append(*a, new(snidpos))
	} else {
		l++
		*a = (*a)[:l]
	}

	if (*a)[l-1] == nil {
		(*a)[l-1] = new(snidpos)
	}

	return (*a)[l-1]
}

/*func (da *Cedar) FindOne(key []byte) (value interface{}, err error) {
	tnid := -1
	snid := -1
	spos := 0
	nid := 0
	e := len(key) - 1

	for i := 0; i <= e; i++ {
		b := key[i]
		if da.hasLabel(nid, b) {
			if da.hasLabel(nid, '*') {
				snid, _ = da.child(nid, '*')
				spos = i
			}

			nid, _ = da.child(nid, b)
			if da.isEnd(nid) {
				if i == e {
					tnid = nid
					break
				} else if snid >= 0 {
					nid = snid
					i = spos
					spos++
				}
			}
		} else if da.hasLabel(nid, '*') {
			nid, _ = da.child(nid, '*')
			snid = nid
			spos = i
			i--
			if da.isEnd(nid) {
				tnid = nid
				break
			}
		} else if snid >= 0 {
			nid = snid
			i = spos
			spos++
		} else {
			return nil, ErrNoPath
		}
	}

	if tnid == -1 && da.hasLabel(nid, '*') {
		nid, _ = da.child(nid, '*')
		if da.isEnd(nid) {
			tnid = nid
		}
	}

	if tnid == -1 {
		return nil, ErrNoPath
	}

	vk, err := da.vKeyOf(tnid)
	if err != nil {
		return nil, ErrNoValue
	}
	if v, ok := da.vals[vk]; ok {
		return v.Value, nil
	}
	return nil, ErrNoValue
}*/

// FindOne works like Get but interpret node label * as wildcard
func (da *Cedar) FindOne(key []byte) (value interface{}, err error) {
	tnid := -1
	nid := 0
	e := len(key) - 1
	var sp *snidpos

	ss := ssPool.Get().(*[]*snidpos)
	defer func() {
		*ss = (*ss)[:0]
		ssPool.Put(ss)
	}()

	sp = getsnidpos(ss)
	sp.nid = 0
	sp.pos = 0

	m := mPool.Get().(*map[int]struct{})
	defer func() {
		for k := range *m {
			delete(*m, k)
		}
		mPool.Put(m)
	}()

ssLoop:
	for len(*ss) > 0 {
		sp = (*ss)[len(*ss)-1]
		nid = sp.nid
		pos := sp.pos

		if sp.nid == 0 {
			*ss = (*ss)[:len(*ss)-1]
			sp = nil
		}

		for i := pos; i <= e; i++ {
			if _, ok := (*m)[nid]; !ok && da.hasLabel(nid, '*') {
				(*m)[nid] = struct{}{}
				spnid, _ := da.child(nid, '*')
				if da.isEnd(spnid) {
					tnid = spnid
					break ssLoop
				} else if i < e {
					sp := getsnidpos(ss)
					sp.pos = i + 1
					sp.nid = spnid
				}
			}

			b := key[i]
			if b != '*' && da.hasLabel(nid, b) {
				nid, _ = da.child(nid, b)
				if i == e {
					if da.isEnd(nid) {
						tnid = nid
						break ssLoop
					} else {
						if sp != nil {
							*ss = (*ss)[:len(*ss)-1]
						}

						snid := nid
						for {
							if !da.hasLabel(snid, '*') {
								break
							}
							snid, _ = da.child(snid, '*')
							if da.isEnd(snid) {
								tnid = snid
								break ssLoop
							}
						}

						break
					}
				}
			} else if sp != nil {
				sp.pos++
				if sp.pos > e {
					*ss = (*ss)[:len(*ss)-1]
				}
				break
			} else {
				break
			}
		}
	}

	if tnid == -1 {
		return nil, ErrNoPath
	}

	vk, err := da.vKeyOf(tnid)
	if err != nil {
		return nil, ErrNoValue
	}
	if v, ok := da.vals[vk]; ok {
		return v.Value, nil
	}
	return nil, ErrNoValue
}

func (da *Cedar) FindAll(key []byte, valCb func(val interface{}, rule []byte)) {
	nid := 0
	e := len(key) - 1
	var sp *snidpos

	ss := ssPool.Get().(*[]*snidpos)
	defer func() {
		*ss = (*ss)[:0]
		ssPool.Put(ss)
	}()

	sp = getsnidpos(ss)
	sp.nid = 0
	sp.pos = 0

	m := mPool.Get().(*map[int]struct{})
	defer func() {
		for k := range *m {
			delete(*m, k)
		}
		mPool.Put(m)
	}()

ssLoop:
	for len(*ss) > 0 {
		sp = (*ss)[len(*ss)-1]
		nid = sp.nid
		pos := sp.pos

		if sp.nid == 0 {
			*ss = (*ss)[:len(*ss)-1]
			sp = nil
		}

		for i := pos; i <= e; i++ {
			if _, ok := (*m)[nid]; !ok && da.hasLabel(nid, '*') {
				(*m)[nid] = struct{}{}
				spnid, _ := da.child(nid, '*')
				if da.isEnd(spnid) {
					vk, err := da.vKeyOf(spnid)
					if err != nil {
						continue ssLoop
					}
					if v, ok := da.vals[vk]; ok {
						rule, _ := da.Key(spnid)
						valCb(v.Value, rule)
					}
					continue ssLoop
				} else if i < e {
					sp := getsnidpos(ss)
					sp.pos = i + 1
					sp.nid = spnid
				}
			}

			b := key[i]
			if b != '*' && da.hasLabel(nid, b) {
				nid, _ = da.child(nid, b)

				if i == e {
					snid := nid
					for {
						if !da.hasLabel(snid, '*') {
							break
						}
						snid, _ = da.child(snid, '*')
						if da.isEnd(snid) {
							vk, err := da.vKeyOf(snid)
							if err != nil {
								continue
							}
							if v, ok := da.vals[vk]; ok {
								rule, _ := da.Key(snid)
								valCb(v.Value, rule)
							}
						}
					}

					if da.isEnd(nid) {
						if sp != nil {
							*ss = (*ss)[:len(*ss)-1]
						}

						vk, err := da.vKeyOf(nid)
						if err != nil {
							continue ssLoop
						}
						if v, ok := da.vals[vk]; ok {
							rule, _ := da.Key(nid)
							valCb(v.Value, rule)
						}
						continue ssLoop
					}

					if sp != nil {
						sp.pos++
						if sp.pos > e {
							*ss = (*ss)[:len(*ss)-1]
						}
					}
				}
			} else if sp != nil {
				sp.pos++
				if sp.pos > e {
					*ss = (*ss)[:len(*ss)-1]
				}
				break
			} else {
				break
			}
		}
	}

	return
}

// PrefixMatch returns a list of at most `num` nodes which match the prefix of the key.
// If `num` is 0, it returns all matches.
// For example, if the following keys were inserted:
//	id	key
//	19	abc
//	23	ab
//	37	abcd
// then
//	PrefixMatch([]byte("abc"), 1) = [ 23 ]				// match ["ab"]
//	PrefixMatch([]byte("abcd"), 0) = [ 23, 19, 37]		// match ["ab", "abc", "abcd"]
func (da *Cedar) PrefixMatch(key []byte, num int) (ids []int) {
	for from, i := 0, 0; i < len(key); i++ {
		to, err := da.Jump(key[i:i+1], from)
		if err != nil {
			break
		}
		if _, err := da.vKeyOf(to); err == nil {
			ids = append(ids, to)
			num--
			if num == 0 {
				return
			}
		}
		from = to
	}
	return
}

// PrefixPredict returns a list of at most `num` nodes which has the key as their prefix.
// These nodes are ordered by their keys.
// If `num` is 0, it returns all matches.
// For example, if the following keys were inserted:
//	id	key
//	19	abc
//	23	ab
//	37	abcd
// then
//	PrefixPredict([]byte("ab"), 2) = [ 23, 19 ]			// predict ["ab", "abc"]
//	PrefixPredict([]byte("ab"), 0) = [ 23, 19, 37 ]		// predict ["ab", "abc", "abcd"]
func (da *Cedar) PrefixPredict(key []byte, num int) (ids []int) {
	root, err := da.Jump(key, 0)
	if err != nil {
		return
	}
	for from, err := da.begin(root); err == nil; from, err = da.next(from, root) {
		ids = append(ids, from)
		num--
		if num == 0 {
			return
		}
	}
	return
}

func (da *Cedar) begin(from int) (to int, err error) {
	for c := da.info[from].Child; c != 0; {
		to = da.array[from].base() ^ int(c)
		c = da.info[to].Child
		from = to
	}
	if da.array[from].base() > 0 {
		return da.array[from].base(), nil
	}
	return from, nil
}

func (da *Cedar) next(from int, root int) (to int, err error) {
	c := da.info[from].Sibling
	for c == 0 && from != root && da.array[from].Check >= 0 {
		from = da.array[from].Check
		c = da.info[from].Sibling
	}
	if from == root {
		return 0, ErrNoPath
	}
	from = da.array[da.array[from].Check].base() ^ int(c)
	return da.begin(from)
}
