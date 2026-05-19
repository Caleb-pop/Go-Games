package main

// Maze definition — mirrors frontend/src/main.ts buildMaze() so that ghosts
// can collision-check against walls without a round-trip to the frontend.
// Keep these two in sync if the layout changes.

const (
	mazeCols = 28
	mazeRows = 31
)

var mazeWalls = buildMazeWalls()

func buildMazeWalls() [][]bool {
	grid := make([][]bool, mazeRows)
	for y := 0; y < mazeRows; y++ {
		grid[y] = make([]bool, mazeCols)
		for x := 0; x < mazeCols; x++ {
			border := x == 0 || y == 0 || x == mazeCols-1 || y == mazeRows-1
			grid[y][x] = border
		}
	}
	blocks := [][4]int{
		{3, 3, 5, 2},
		{10, 3, 8, 2},
		{22, 3, 3, 2},
		{3, 8, 3, 5},
		{10, 8, 8, 2},
		{22, 8, 3, 5},
		{3, 18, 3, 5},
		{10, 18, 8, 2},
		{22, 18, 3, 5},
		{3, 27, 5, 2},
		{10, 27, 8, 2},
		{22, 27, 3, 2},
	}
	for _, b := range blocks {
		x, y, w, h := b[0], b[1], b[2], b[3]
		for dy := 0; dy < h; dy++ {
			for dx := 0; dx < w; dx++ {
				if y+dy >= 0 && y+dy < mazeRows && x+dx >= 0 && x+dx < mazeCols {
					grid[y+dy][x+dx] = true
				}
			}
		}
	}
	return grid
}

// isWall reports whether the tile at (x, y) is a wall or out of bounds.
func isWall(x, y int) bool {
	if x < 0 || y < 0 || x >= mazeCols || y >= mazeRows {
		return true
	}
	return mazeWalls[y][x]
}
