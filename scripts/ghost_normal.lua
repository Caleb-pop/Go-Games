-- ghost_normal.lua
-- Chase the player by preferring the axis with the greater delta.
-- Returns one of: "up", "down", "left", "right"

function think(ghost_id, gx, gy, state, dt)
    local px, py = get_player_position()
    local dx = px - gx
    local dy = py - gy

    if math.abs(dx) >= math.abs(dy) then
        if dx > 0 then return "right"
        elseif dx < 0 then return "left"
        end
    end

    if dy > 0 then return "down"
    elseif dy < 0 then return "up"
    end

    -- Same tile: hold still this tick
    return "none"
end
