package main

type point struct {
	x, y int
}

func pt(x, y int) point { return point{x: x, y: y} }

type byX []point

func (p byX) Len() int           { return len(p) }
func (p byX) Less(i, j int) bool { return p[i].x < p[j].x }
func (p byX) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

type byY []point

func (p byY) Len() int           { return len(p) }
func (p byY) Less(i, j int) bool { return p[i].y < p[j].y }
func (p byY) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
