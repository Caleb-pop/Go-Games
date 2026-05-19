import './style.css';
import { Application, Container, Graphics, Text, TextStyle } from 'pixi.js';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { ScareGhosts, ResetGhosts, SetPlayerPosition } from '../wailsjs/go/main/GameEngine';

// --- Maze constants ---
const TILE = 24;
const COLS = 28;
const ROWS = 31;

const MAZE: number[][] = buildMaze();

// Pellet state — 0=open, 1=wall, 2=pellet eaten, 3=power pellet eaten
const PELLETS: number[][] = buildPellets();

function buildMaze(): number[][] {
    const grid: number[][] = [];
    for (let y = 0; y < ROWS; y++) {
        const row: number[] = [];
        for (let x = 0; x < COLS; x++) {
            const border = x === 0 || y === 0 || x === COLS - 1 || y === ROWS - 1;
            row.push(border ? 1 : 0);
        }
        grid.push(row);
    }
    const blocks: Array<[number, number, number, number]> = [
        [3, 3, 5, 2],
        [10, 3, 8, 2],
        [22, 3, 3, 2],
        [3, 8, 3, 5],
        [10, 8, 8, 2],
        [22, 8, 3, 5],
        [3, 18, 3, 5],
        [10, 18, 8, 2],
        [22, 18, 3, 5],
        [3, 27, 5, 2],
        [10, 27, 8, 2],
        [22, 27, 3, 2],
    ];
    for (const [x, y, w, h] of blocks) {
        for (let dy = 0; dy < h; dy++) {
            for (let dx = 0; dx < w; dx++) {
                if (grid[y + dy] && grid[y + dy][x + dx] !== undefined) {
                    grid[y + dy][x + dx] = 1;
                }
            }
        }
    }
    return grid;
}

function buildPellets(): number[][] {
    // Copy maze — open tiles get pellets, walls stay walls
    return MAZE.map(row => [...row]);
}

// Power pellet positions (corners)
const POWER_PELLETS: Array<[number, number]> = [
    [2, 2], [25, 2], [2, 28], [25, 28]
];

function isWall(x: number, y: number): boolean {
    if (x < 0 || y < 0 || x >= COLS || y >= ROWS) return true;
    return MAZE[y][x] === 1;
}

const GHOST_COLORS: Record<string, number> = {
    Spot:     0x0066b3, // Bayern blue
    Tracker:  0xffffff, // white
    Shadow:   0x2d7d2d, // forest green
    Prowler:  0x8b4513, // saddle brown
};
const SCARED_COLOR  = 0x6a0dad; // deep purple — distinct from any base color

type GhostUpdate = {
    ID: string;
    X: number;
    Y: number;
    State: number; // 0=normal 1=scared 2=eaten
    Dir: number;
};

// --- Score ---
let score = 0;
let lives = 3;

async function main() {
    const app = new Application();
    await app.init({
        width:      COLS * TILE,
        height:     ROWS * TILE + 40, // extra bar for score/lives
        background: 0x000000,
        antialias:  true,
    });
    document.getElementById('app')!.appendChild(app.canvas);

    // --- Score bar ---
    const scoreStyle = new TextStyle({ fill: 0xffffff, fontSize: 16, fontFamily: 'monospace' });
    const scoreText  = new Text({ text: 'SCORE  0', style: scoreStyle });
    scoreText.x = 8;
    scoreText.y = ROWS * TILE + 8;
    app.stage.addChild(scoreText);

    const livesStyle = new TextStyle({ fill: 0xffff00, fontSize: 16, fontFamily: 'monospace' });
    const livesText  = new Text({ text: `LIVES  ${lives}`, style: livesStyle });
    livesText.x = COLS * TILE - 120;
    livesText.y = ROWS * TILE + 8;
    app.stage.addChild(livesText);

    // --- Maze ---
    drawMaze(app.stage);

    // --- Pellet layer ---
    const pelletLayer = new Container();
    app.stage.addChild(pelletLayer);
    drawPellets(pelletLayer);

    // --- Ghost layer ---
    const ghostLayer = new Container();
    app.stage.addChild(ghostLayer);
    const ghostSprites = new Map<string, Graphics>();

    // --- Player ---
    let playerX   = 14;
    let playerY   = 23;
    let mouthOpen = true;
    let facing    = 0; // radians: 0=right, π=left, π/2=down, -π/2=up

    const player = new Graphics();
    app.stage.addChild(player);
    drawPlayer(player, mouthOpen, facing);
    placeAtTile(player, playerX, playerY);
    SetPlayerPosition(playerX, playerY);

    // Mouth animation — throttle by counting ticks, not by clamping the renderer
    let mouthTick = 0;
    app.ticker.add(() => {
        if (++mouthTick % 8 === 0) {
            mouthOpen = !mouthOpen;
            drawPlayer(player, mouthOpen, facing);
        }
    });

    // --- Keyboard ---
    // WebView2 sometimes loads without focus on the document body — grab it
    // explicitly so keydown actually fires.
    document.body.tabIndex = 0;
    document.body.focus();
    window.addEventListener('click', () => document.body.focus());

    // Track which direction keys are currently held. Movement is driven by a
    // fixed-cadence stepper below, not by the OS keydown repeat — that way the
    // first move and every subsequent move are evenly spaced.
    const heldDirs: string[] = []; // most-recent at the end
    const DIR_KEYS = new Set(['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight']);

    window.addEventListener('keydown', (e) => {
        if (DIR_KEYS.has(e.key)) {
            e.preventDefault();
            const i = heldDirs.indexOf(e.key);
            if (i !== -1) heldDirs.splice(i, 1);
            heldDirs.push(e.key);
            return;
        }
        if (e.key === ' ')                 { ScareGhosts(); return; }
        if (e.key === 'r' || e.key === 'R'){ ResetGhosts(); return; }
    });
    window.addEventListener('keyup', (e) => {
        const i = heldDirs.indexOf(e.key);
        if (i !== -1) heldDirs.splice(i, 1);
    });

    function step() {
        const dir = heldDirs[heldDirs.length - 1];
        if (!dir) return;
        let nx = playerX, ny = playerY;
        switch (dir) {
            case 'ArrowUp':    ny--; facing = -Math.PI / 2; break;
            case 'ArrowDown':  ny++; facing =  Math.PI / 2; break;
            case 'ArrowLeft':  nx--; facing =  Math.PI;     break;
            case 'ArrowRight': nx++; facing =  0;           break;
        }
        if (isWall(nx, ny)) return;
        playerX = nx;
        playerY = ny;
        placeAtTile(player, playerX, playerY);
        SetPlayerPosition(playerX, playerY);

        if (PELLETS[ny][nx] === 0) {
            PELLETS[ny][nx] = 2;
            score += isPowerPellet(nx, ny) ? 50 : 10;
            scoreText.text = `SCORE  ${score}`;
            drawPellets(pelletLayer);
            if (isPowerPellet(nx, ny)) ScareGhosts();
        }
    }
    setInterval(step, 150);

    // --- Ghost updates from Go ---
    EventsOn('ghost:update', (u: GhostUpdate) => {
        let sprite = ghostSprites.get(u.ID);
        if (!sprite) {
            sprite = new Graphics();
            ghostLayer.addChild(sprite);
            ghostSprites.set(u.ID, sprite);
        }
        const color = u.State === 1
            ? SCARED_COLOR
            : u.State === 2
                ? 0x444444
                : (GHOST_COLORS[u.ID] ?? 0xffffff);

        drawGhost(sprite, color, u.State === 2);
        placeAtTile(sprite, u.X, u.Y);
    });
}

const LEPARD_GOLD = 0xd4a017;
const LEPARD_SPOT = 0x3b2412;

function drawPlayer(g: Graphics, mouthOpen: boolean, angle: number) {
    g.clear();
    const r = TILE / 2 - 2;
    if (mouthOpen) {
        const mouthAngle = Math.PI / 5;
        g.moveTo(0, 0);
        g.arc(0, 0, r, angle + mouthAngle, angle + Math.PI * 2 - mouthAngle);
        g.lineTo(0, 0);
        g.fill(LEPARD_GOLD);
    } else {
        g.circle(0, 0, r).fill(LEPARD_GOLD);
    }
    // Leopard spots — small dark rosettes scattered on the body
    const spots: Array<[number, number, number]> = [
        [-r * 0.45, -r * 0.10, 1.6],
        [-r * 0.10,  r * 0.40, 1.4],
        [ r * 0.35,  r * 0.20, 1.5],
        [-r * 0.30,  r * 0.55, 1.2],
    ];
    for (const [sx, sy, sr] of spots) {
        g.circle(sx, sy, sr).fill(LEPARD_SPOT);
    }
    // Eye
    const eyeX = Math.cos(angle - Math.PI / 4) * (r * 0.5);
    const eyeY = Math.sin(angle - Math.PI / 4) * (r * 0.5);
    g.circle(eyeX, eyeY, 2).fill(0x000000);
}

// --- Draw ghost shape ---
function drawGhost(g: Graphics, color: number, eaten: boolean) {
    g.clear();
    if (eaten) {
        // Just eyes when eaten
        g.circle(-4, -3, 4).fill(0xffffff);
        g.circle( 4, -3, 4).fill(0xffffff);
        g.circle(-3, -3, 2).fill(0x0000ff);
        g.circle( 5, -3, 2).fill(0x0000ff);
        return;
    }
    const s = TILE / 2 - 2;
    // Body — dome top, wavy bottom
    g.moveTo(-s, s);
    g.lineTo(-s, -s + 4);
    g.arc(0, -s + 4, s, Math.PI, 0); // dome
    g.lineTo(s, s);
    // Wavy skirt (3 bumps)
    const bumpW = (s * 2) / 3;
    g.arc(s - bumpW * 0.5, s, bumpW * 0.5, 0, Math.PI);
    g.arc(s - bumpW * 1.5, s, bumpW * 0.5, 0, Math.PI);
    g.arc(s - bumpW * 2.5, s, bumpW * 0.5, 0, Math.PI);
    g.fill(color);
    // Eyes
    g.circle(-s * 0.4, -s * 0.2, 3.5).fill(0xffffff);
    g.circle( s * 0.4, -s * 0.2, 3.5).fill(0xffffff);
    g.circle(-s * 0.3, -s * 0.2, 2).fill(0x222299);
    g.circle( s * 0.5, -s * 0.2, 2).fill(0x222299);
}

// --- Draw all uneaten pellets ---
function drawPellets(layer: Container) {
    layer.removeChildren();
    const g = new Graphics();
    for (let y = 0; y < ROWS; y++) {
        for (let x = 0; x < COLS; x++) {
            if (PELLETS[y][x] === 0) {
                if (isPowerPellet(x, y)) {
                    g.circle(x * TILE + TILE / 2, y * TILE + TILE / 2, 5).fill(0xffd700);
                } else {
                    g.circle(x * TILE + TILE / 2, y * TILE + TILE / 2, 2).fill(0xffddaa);
                }
            }
        }
    }
    layer.addChild(g);
}

function isPowerPellet(x: number, y: number): boolean {
    return POWER_PELLETS.some(([px, py]) => px === x && py === y);
}

function placeAtTile(g: Graphics, x: number, y: number) {
    g.x = x * TILE + TILE / 2;
    g.y = y * TILE + TILE / 2;
}

function drawMaze(stage: Container) {
    const g = new Graphics();
    for (let y = 0; y < ROWS; y++) {
        for (let x = 0; x < COLS; x++) {
            if (MAZE[y][x] === 1) {
                g.rect(x * TILE, y * TILE, TILE, TILE);
            }
        }
    }
    g.fill(0x1a1aff);
    // Blue border glow lines on wall edges (classic look)
    g.stroke({ color: 0x4444ff, width: 1 });
    stage.addChild(g);
}

main().catch((err) => console.error(err));