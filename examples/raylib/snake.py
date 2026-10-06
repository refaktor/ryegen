# Minimal Snake game in Python using raylib
# Install dependency: pip install raylib
# Run with: python snake.py

import pyray as rl
import random

# Constants
W = 800
H = 600
CELL = 20
GW = 40   # grid width
GH = 30   # grid height

# State
sx = [10, 9, 8, 7, 6]
sy = [15, 15, 15, 15, 15]
dx = 1
dy = 0
fx = 20
fy = 10
pts = 0
dead = False
tick = 0

rl.init_window(W, H, "Snake - Python + Raylib")
rl.set_target_fps(60)

while not rl.window_should_close():

    if dead:
        # Restart
        if rl.is_key_pressed(rl.KEY_ENTER):
            sx = [10, 9, 8, 7, 6]
            sy = [15, 15, 15, 15, 15]
            dx = 1
            dy = 0
            pts = 0
            dead = False
            fx = random.randint(0, GW - 1)
            fy = random.randint(0, GH - 1)
    else:
        # Input - prevent 180 degree turns
        if rl.is_key_pressed(rl.KEY_UP)    and dy != 1:
            dx, dy = 0, -1
        if rl.is_key_pressed(rl.KEY_DOWN)  and dy != -1:
            dx, dy = 0, 1
        if rl.is_key_pressed(rl.KEY_LEFT)  and dx != 1:
            dx, dy = -1, 0
        if rl.is_key_pressed(rl.KEY_RIGHT) and dx != -1:
            dx, dy = 1, 0

        # Update every 6 frames
        tick += 1
        if tick > 5:
            tick = 0

            # Calculate new head
            hx = sx[0] + dx
            hy = sy[0] + dy

            # Wrap around
            if hx < 0:  hx = GW - 1
            if hx >= GW: hx = 0
            if hy < 0:  hy = GH - 1
            if hy >= GH: hy = 0

            # Check self-collision
            for i in range(len(sx)):
                if sx[i] == hx and sy[i] == hy:
                    dead = True

            if not dead:
                # Did we eat?
                ate = (hx == fx and hy == fy)

                # Move: prepend head, maybe remove tail
                sx.insert(0, hx)
                sy.insert(0, hy)

                if ate:
                    pts += 10
                    fx = random.randint(0, GW - 1)
                    fy = random.randint(0, GH - 1)
                else:
                    # Remove tail
                    sx.pop()
                    sy.pop()

    # === DRAW ===
    rl.begin_drawing()
    rl.clear_background(rl.BLACK)

    if dead:
        rl.draw_text("GAME OVER", 280, 250, 50, rl.RED)
        rl.draw_text(f"Score: {pts}", 330, 310, 30, rl.WHITE)
        rl.draw_text("Press ENTER to restart", 250, 370, 20, rl.GRAY)
    else:
        # Draw food
        rl.draw_rectangle(fx * CELL, fy * CELL, CELL - 2, CELL - 2, rl.RED)

        # Draw snake
        for i in range(len(sx)):
            clr = rl.LIME if i == 0 else rl.GREEN
            rl.draw_rectangle(sx[i] * CELL, sy[i] * CELL, CELL - 2, CELL - 2, clr)

        # Draw score
        rl.draw_text(f"Score: {pts}", 10, 10, 20, rl.WHITE)

    rl.end_drawing()

rl.close_window()
