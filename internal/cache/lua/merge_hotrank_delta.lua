local delta_map = redis.call("HGETALL", KEYS[1])
if #delta_map == 0 then
    return 0
end

for i = 1, #delta_map, 2 do
    local member = delta_map[i]
    local delta = tonumber(delta_map[i + 1]) or 0
    if delta ~= 0 then
        redis.call("ZINCRBY", KEYS[2], delta, member)
    end
end

redis.call("DEL", KEYS[1])
return #delta_map / 2
