# Breakout

A small breakout game built on Overdrive: the scene is TOML, the game logic is one `gameobject` script per kind of object, the HUD is a gutter tree. Everything lives in this folder.

```sh
cd demo
go run ./breakout          # play
go run ./breakout -demo    # the paddle plays itself
go run ./breakout -demo -screenshot out.png   # frame 90, 1.5 s into a demo
```

`-config` works as for the engine (`configs/vulkan.toml` by default).

**Controls:** A / D, the arrows or the mouse move the paddle. Space or a click launches the ball. Space, or the PLAY AGAIN button, restarts after the game ends. Escape quits.

## The folder

| file | what it is |
| --- | --- |
| `main.go` | loading: finds the meshes, adds the walls, lays the bricks out, adds the objects in update order |
| `rules.go` | `Rules`: state, score, lives, the ball's speed, the launch press, restart |
| `paddle.go` | `Paddle`: keys, cursor or demo → `Teleport` |
| `brick.go` | `Brick`: breaks on its first contact with the ball |
| `ball.go` | `Ball`: serve, re-aim off the paddle, keep the rules' speed, roll, lose a life |
| `hud.go` | the HUD, rebuilt every frame from `Rules`; not a game object |
| `make_meshes.py` | writes the box OBJs into `scene/meshes/`. **Sizes live here**: the game reads them back from the vertices |
| `scene/breakout.toml` | the scene, written by hand in the exporter's format (Blender z-up axes) |
| `scene/meshes/` | the generated boxes, their MTLs (one per brick row), and a copy of the showcase's `Football.obj` |
| `ui/` | the HUD's images: framed panels (blue, grey, yellow) behind every text, nine-sliced so their frames keep their width at any size, and the end panel's buttons, green and red, raised and flat (the flat one shows on hover). From Kenney's [UI Pack](https://kenney.nl/assets/ui-pack), CC0: `ui/KENNEY_LICENSE.txt` |
| `scene/textures` | a **symlink** to `assets/showcase/textures`, so the 4K maps are not copied |

## How it uses the engine

- **Scene.** The floor and walls are static, placed where they stand like an export. The ball, paddle and the 24 bricks are `movable = true`, so the engine puts their shadows in the dynamic atlas from the start. The bricks and paddle are built about the origin, which makes `Mesh.Place(centre, rotation)` put them exactly at `centre`. The ball keeps the showcase's position in the TOML, because that's where its OBJ's vertices are, and `Place` carries it from there.
- **Game logic.** Four `gameobject.GameObject` scripts. `App.Run` starts them before the first frame and calls their `Update` at the physics rate (120 Hz), before each `physics.World` step, in the order `load` added them: `Rules` (reads the launch press), `Paddle` (moves), the bricks (break), then `Ball` (sees the paddle where it moved and the speed the bricks set). Each finds the others it needs in `Start` with `gameobject.Find`.
- **Physics.** The bounces are `physics.World`'s. The ball is a sphere with no gravity, elastic and frictionless (`Restitution` 1, `Friction` 0, `RestingSpeed` 0), and the walls, bricks and paddle are boxes. The game only steers it:
  - Each brick reads `World.ContactsOf` its own body, and breaks on a sphere (`World.Remove` plus hiding the mesh). The ball reads its own contacts, and one on the paddle's top face re-aims it by where it landed.
  - It then resets the speed with `Velocity`/`SetVelocity`, since breakout's speed is a rule, not physics.
  - The paddle is a fixed box the game `Teleport`s: teleported rather than driven through `Pos`, so the ball takes none of its speed and the game aims the ball itself. The world places every body's mesh when the body moves, the paddle's and the bricks' included, and the game sets the ball's `Orientation` so it rolls.
  - A broken brick is hidden with `Mesh.SetVisible(false)`, which takes it out of every draw and shadow bake, and shown again on restart.
- **Materials.** The second row alternates polished gold and chrome (`BrickGold.mtl`, `BrickChrome.mtl`): plain MTL values, fully metallic, roughness 0.15 and 0.10, no textures. The environment `strength` is 1.5, so the metals have a brighter sky to mirror. The engine scales image-based light by 0.25 (`environmentLightScale`), so a polished metal reflects a sky four times dimmer than the one drawn. Their front faces still look dark: they mirror the ground below the camera, and the engine has no screen-space reflections to show the floor there.
- **Lights.** A sun, orange and blue lights at the back corners, and four low coloured `Glint` lights: magenta, amber and cyan over the back wall, violet behind the paddle. A flat top seen from this camera mirrors a light that is low and behind it into the view, which is where the metals' highlights come from.
- **Bricks** are laid out in `load` from the walls' inner faces: six to a row, the rows in TOML order (the back row first, worth the most).
- **Input.** `NewApp` gets a no-op input handler, which replaces the default fly camera, and the game's own cursor callback. Keys are read in the scripts' `Update`. While the paddle is in play the cursor is captured and its motion moves the paddle. On the end screen it is freed, which is what lets the overlay click the buttons.
- **HUD.** `hud.build` is rebuilt every frame from `Rules`, and counts its own frames for the FPS panel, published once a second so the label's bitmap changes only then. A gutter `Stack` layers the end panel over the HUD, and `StyleText.Align` centres the panel's text and the buttons' labels.

## Changes to Overdrive and gutter

Made for this game, each additive and checked against the showcase: frames 90, 330 and 360 are pixel-identical to captures taken before.

**Overdrive**

1. **`App.StartDelay`** (`core/app.go`). The five seconds physics and entities wait before stepping was a constant (`physicsStartDelay`), so a game started with its ball frozen. It is now an `App` field that `NewApp` sets to that same constant, and the game sets it to 0. The screenshot step count reads the field too. CLAUDE.md, `notes/FEATURES.md` and `notes/OVERVIEW.md` now name the field.
2. **Contacts** (`physics/world.go`). `Collider.Collide` now returns the contact normal and whether the two touched. `World.Step` records each touching pair once, and `World.Contacts()` / `World.ContactsOf(body)` hand them to the game. Entities update before each step, so a game sees every contact exactly once, one step late.
3. **`World.Remove(bodies...)`**, between steps.
4. **Velocity in m/s** (`physics/verlet.go`). `Velocity(step)` and `SetVelocity(v, step)` convert to and from Verlet's per-step displacement. `Teleport(pos)` moves a body and keeps its velocity, where setting `Pos` alone would launch it.
5. **`Sphere.RestingSpeed`** (`physics/sphere.go`). The rebound threshold was a constant, which stopped an elastic ball dead on a grazing hit. `NewSphere` sets the old value, and a game can set 0.
6. **`NewSphere` defaults `Inertia` to 2/5.** At 0 a frictionless contact computed 0 × ∞ and made the spin NaN.

**Gutter** (`../gutter`, a `replace` in `go.mod`)

1. **`StyleText.Align`**: `TextLeft` (the zero value, so existing text is unchanged), `TextCenter`, `TextRight`. The text cache now keeps the string's measured width, since the bitmap's own is rounded up to 64 px and would centre it off by up to 32 px.
2. **`Stack`** (`ui/stack.go`): its children laid over one box, the last on top, each placed by its own `Size` and `Alignment`.
3. **`Style.Slice`**: draws a widget's `Image` as a 9-slice. The corners keep their size, the edges stretch along their length, and only the middle stretches both ways, so one framed panel image fits the 3:1 score box and the 12:1 hint bar alike. `slicedTexture` (`ui/drawlist.go`) cuts the image into nine cached textures, so the host still draws whole-texture quads and Overdrive needed no change. The engine's sampler wraps rather than clamps, which leaves no seams here: each corner is drawn 1:1 and each edge only along its length, where its pixels repeat.

README and OVERVIEW list all three. Gutter's Vulkan example still builds.

## Engine gaps it works around

Not changed, but worth knowing:

- **Textures resolve to `textures/` beside the scene, by basename** (`scene/mesh.go:texturePath`). A scene can't point at another scene's maps, hence the symlink.
- **Meshes can't be instanced or cloned at runtime.** All 24 bricks are separate `[[mesh]]` entries that load the same OBJ, and their textures are decoded once because `decodeTextures` dedups by path.
- **`NewApp` installs a scroll callback that zooms the camera** (`input.ScrollCallback`). The game removes it.
