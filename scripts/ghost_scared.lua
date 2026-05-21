-- ghost_scared.lua
-- Flee from the player by moving away on the dominant axis.
-- Returns one of: "up", "down", "left", "right"

function think(ghost_id, gx, gy, state, dt)
    local px, py = get_player_position()
    local dx = gx - px   -- reversed: we want to move AWAY
    local dy = gy - py

    if math.abs(dx) >= math.abs(dy) then
        if dx > 0 then return "right"
        elseif dx < 0 then return "left"
        end
    end

    if dy > 0 then return "down"
    elseif dy < 0 then return "up"
    end

    return "none"
end
