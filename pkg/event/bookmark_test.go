package event

import "testing"

func TestBookmarkClone(t *testing.T) {
	var bm *Bookmark
	if bm.Clone() != nil {
		t.Fatal("nil clone should be nil")
	}
	bm = &Bookmark{Seq: 42}
	clone := bm.Clone()
	if clone == bm {
		t.Fatal("clone should not share pointer")
	}
	if clone.Seq != bm.Seq {
		t.Fatalf("clone seq mismatch: %d", clone.Seq)
	}
}

func TestCompareBookmark(t *testing.T) {
	cases := []struct {
		a, b     *Bookmark
		expected int
	}{
		{nil, nil, 0},
		{nil, &Bookmark{Seq: 1}, -1},
		{&Bookmark{Seq: 1}, nil, 1},
		{&Bookmark{Seq: 1}, &Bookmark{Seq: 2}, -1},
		{&Bookmark{Seq: 3}, &Bookmark{Seq: 2}, 1},
		{&Bookmark{Seq: 5}, &Bookmark{Seq: 5}, 0},
	}
	for i, c := range cases {
		if got := compareBookmark(c.a, c.b); got != c.expected {
			t.Fatalf("case %d: expected %d got %d", i, c.expected, got)
		}
	}
}

func TestNewBookmarkHelper(t *testing.T) {
	bm := newBookmark(-1)
	if bm.Seq != 0 {
		t.Fatalf("seq should clamp to 0, got %d", bm.Seq)
	}
	if bm.Timestamp.IsZero() {
		t.Fatal("timestamp should be set")
	}
}
