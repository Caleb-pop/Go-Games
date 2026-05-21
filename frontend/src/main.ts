import './style.css';
import { Application, Assets, Container, Graphics, Sprite, Text, TextStyle, Texture } from 'pixi.js';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { GetPellets, GetPlayerPosition, GetState, MovePlayer, ResetGhosts } from '../wailsjs/go/main/GameEngine';

import playerRightUrl from './assets/sprites/playerRight.png';
import playerLeftUrl  from './assets/sprites/playerLeft.png';
import goldCatUrl     from './assets/sprites/goldCat.png';
import blueDragonUrl  from './assets/sprites/blueDragon.png';
import blackCatsUrl   from './assets/sprites/blackCats.png';

// --- Maze constants (rendering only — layout is authoritative in Go maze.go) ---
const TILE = 24;
const COLS = 28;
const ROWS = 31;

// Maze wall layout mirrors Go's buildMazeWalls() — used only to draw the maze
// and to give instant visual feedback before the first ghost:update arrives.
// Player movement validation lives in Go; this copy is NOT used for collision.
const MAZE_WALLS: boolean[][] = buildMazeWalls();

function buildMazeWalls(): boolean[][] {
    const grid: boolean[][] = [];
    for (let y = 0; y < ROWS; y++) {
        const row: boolean[] = [];
        for (let x = 0; x < COLS; x++) {
            row.push(x === 0 || y === 0 || x === COLS - 1 || y === ROWS - 1);
        }
        grid.push(row);
    }
    const blocks: Array<[number, number, number, number]> = [
        [3, 3, 5, 2],  [10, 3, 8, 2],  [22, 3, 3, 2],
        [3, 8, 3, 5],  [10, 8, 8, 2],  [22, 8, 3, 5],
        [3, 18, 3, 5], [10, 18, 8, 2], [22, 18, 3, 5],
        [3, 27, 5, 2], [10, 27, 8, 2], [22, 27, 3, 2],
    ];
    for (const [x, y, w, h] of blocks) {
        for (let dy = 0; dy < h; dy++)
            for (let dx = 0; dx < w; dx++)
                if (grid[y + dy]) grid[y + dy][x + dx] = true;
    }
    return grid;
}

const SCARED_COLOR = 0x6a0dad;

type GhostUpdate = { ID: string; X: number; Y: number; State: number; Dir: number };
type PlayerUpdate = { X: number; Y: number; Dir: number };
type GameState    = { Score: number; Lives: number; GameOver: boolean };

// Pellet values returned by Go: 0=present, 1=wall, 2=normal eaten, 3=power eaten
type PelletGrid = number[][];

async function main() {
    const app = new Application();
    await app.init({
        width:      COLS * TILE,
        height:     ROWS * TILE + 40,
        background: 0x000000,
        antialias:  true,
    });
    document.getElementById('app')!.appendChild(app.canvas);

    // --- HUD ---
    const scoreStyle = new TextStyle({ fill: 0xffffff, fontSize: 16, fontFamily: 'monospace' });
    const scoreText  = new Text({ text: 'SCORE  0', style: scoreStyle });
    scoreText.x = 8;
    scoreText.y = ROWS * TILE + 8;
    app.stage.addChild(scoreText);

    const livesStyle = new TextStyle({ fill: 0xffff00, fontSize: 16, fontFamily: 'monospace' });
    const livesText  = new Text({ text: 'LIVES  3', style: livesStyle });
    livesText.x = COLS * TILE - 120;
    livesText.y = ROWS * TILE + 8;
    app.stage.addChild(livesText);

    const overStyle = new TextStyle({ fill: 0xff3333, fontSize: 48, fontFamily: 'monospace', fontWeight: 'bold' });
    const overText  = new Text({ text: 'GAME OVER', style: overStyle });
    overText.anchor.set(0.5);
    overText.x = (COLS * TILE) / 2;
    overText.y = (ROWS * TILE) / 2;
    overText.visible = false;
    app.stage.addChild(overText);

    // --- Static layers ---
    drawMaze(app.stage);

    const pelletLayer = new Container();
    app.stage.addChild(pelletLayer);

    // --- Textures ---
    const [playerRightTex, playerLeftTex, goldCatTex, blueDragonTex, blackCatsTex] = await Promise.all([
        Assets.load<Texture>(playerRightUrl),
        Assets.load<Texture>(playerLeftUrl),
        Assets.load<Texture>(goldCatUrl),
        Assets.load<Texture>(blueDragonUrl),
        Assets.load<Texture>(blackCatsUrl),
    ]);
    const ghostTextures: Record<string, Texture> = {
        Spot:    goldCatTex,
        Tracker: blueDragonTex,
        Shadow:  blackCatsTex,
    };

    function setupTileSprite(s: Sprite) {
        s.anchor.set(0.5);
        s.width  = TILE - 2;
        s.height = TILE - 2;
    }

    // --- Ghost layer ---
    const ghostLayer = new Container();
    app.stage.addChild(ghostLayer);

    // --- Player ---
    const player = new Sprite(playerRightTex);
    setupTileSprite(player);
    app.stage.addChild(player);

    function applyPlayerUpdate(u: PlayerUpdate) {
        placeAtTile(player, u.X, u.Y);
        // Dir: 0=right 1=left 2=up 3=down (up/down borrow the horiz sprites)
        const leftish = u.Dir === 1 || u.Dir === 2;
        player.texture = leftish ? playerLeftTex : playerRightTex;
    }

    // --- Initial state sync from Go ---
    const [initPlayer, initState, initPellets] = await Promise.all([
        GetPlayerPosition(),
        GetState(),
        GetPellets(),
    ]);
    applyPlayerUpdate(initPlayer);
    applyGameState(initState);
    drawPellets(pelletLayer, initPellets);

    // --- Keyboard ---
    document.body.tabIndex = 0;
    document.body.focus();
    window.addEventListener('click', () => document.body.focus());

    const heldDirs: string[] = [];
    const DIR_KEYS = new Set(['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight']);
    let isGameOver = initState.GameOver;

    window.addEventListener('keydown', (e) => {
        if (DIR_KEYS.has(e.key)) {
            if (isGameOver) return;
            e.preventDefault();
            const i = heldDirs.indexOf(e.key);
            if (i !== -1) heldDirs.splice(i, 1);
            heldDirs.push(e.key);
            return;
        }
        if (e.key === 'r' || e.key === 'R') ResetGhosts();
    });
    window.addEventListener('keyup', (e) => {
        const i = heldDirs.indexOf(e.key);
        if (i !== -1) heldDirs.splice(i, 1);
    });

    // Step: send the held direction to Go; Go validates, updates state, and
    // fires player:update + game:state events. No local position tracking.
    function step() {
        if (isGameOver) return;
        const dir = heldDirs[heldDirs.length - 1];
        if (!dir) return;
        let goDir = '';
        switch (dir) {
            case 'ArrowUp':    goDir = 'up';    break;
            case 'ArrowDown':  goDir = 'down';  break;
            case 'ArrowLeft':  goDir = 'left';  break;
            case 'ArrowRight': goDir = 'right'; break;
        }
        MovePlayer(goDir); // fire-and-forget; response comes via events
    }
    setInterval(step, 150);

    // --- Events from Go ---

    EventsOn('player:update', (u: PlayerUpdate) => {
        applyPlayerUpdate(u);
    });

    EventsOn('game:state', (s: GameState) => {
        applyGameState(s);
        isGameOver = s.GameOver;
    });

    // pellet:update is emitted after every MovePlayer that eats a pellet.
    // Go sends the full grid so we never have to diff locally.
    EventsOn('pellet:update', (grid: PelletGrid) => {
        drawPellets(pelletLayer, grid);
    });

    type GhostNodes = { sprite?: Sprite; eaten: Graphics; current: Container };
    const ghostCache = new Map<string, GhostNodes>();

    EventsOn('ghost:update', (u: GhostUpdate) => {
        let nodes = ghostCache.get(u.ID);
        if (!nodes) {
            const eaten = new Graphics();
            const tex = ghostTextures[u.ID];
            const sprite = tex ? new Sprite(tex) : undefined;
            if (sprite) setupTileSprite(sprite);
            nodes = { sprite, eaten, current: sprite ?? eaten };
            ghostLayer.addChild(nodes.current);
            ghostCache.set(u.ID, nodes);
        }

        const wantEaten = u.State === 2 || !nodes.sprite;
        const desired = wantEaten ? nodes.eaten : nodes.sprite!;
        if (desired !== nodes.current) {
            ghostLayer.removeChild(nodes.current);
            ghostLayer.addChild(desired);
            nodes.current = desired;
        }

        if (wantEaten) {
            drawEatenGhost(nodes.eaten);
        } else {
            nodes.sprite!.tint = u.State === 1 ? SCARED_COLOR : 0xffffff;
        }
        placeAtTile(nodes.current, u.X, u.Y);
    });

    function applyGameState(s: GameState) {
        scoreText.text = `SCORE  ${s.Score}`;
        livesText.text = `LIVES  ${s.Lives}`;
        overText.visible = s.GameOver;
    }
}

// --- Pure rendering helpers ---

function drawEatenGhost(g: Graphics) {
    g.clear();
    g.circle(-4, -3, 4).fill(0xffffff);
    g.circle( 4, -3, 4).fill(0xffffff);
    g.circle(-3, -3, 2).fill(0x0000ff);
    g.circle( 5, -3, 2).fill(0x0000ff);
}

function drawPellets(layer: Container, grid: PelletGrid) {
    layer.removeChildren();
    const g = new Graphics();
    const powerPellets = new Set(['2,2','25,2','2,28','25,28']);
    for (let y = 0; y < ROWS; y++) {
        for (let x = 0; x < COLS; x++) {
            if (grid[y][x] !== 0) continue;
            const cx = x * TILE + TILE / 2;
            const cy = y * TILE + TILE / 2;
            if (powerPellets.has(`${x},${y}`)) {
                g.circle(cx, cy, 5).fill(0xffd700);
            } else {
                g.circle(cx, cy, 2).fill(0xffddaa);
            }
        }
    }
    layer.addChild(g);
}

function placeAtTile(node: Container, x: number, y: number) {
    node.x = x * TILE + TILE / 2;
    node.y = y * TILE + TILE / 2;
}

function drawMaze(stage: Container) {
    const g = new Graphics();
    for (let y = 0; y < ROWS; y++)
        for (let x = 0; x < COLS; x++)
            if (MAZE_WALLS[y][x]) g.rect(x * TILE, y * TILE, TILE, TILE);
    g.fill(0x1a1aff);
    g.stroke({ color: 0x4444ff, width: 1 });
    stage.addChild(g);
}

main().catch((err) => console.error(err));
