local ttl = tonumber(ARGV[1]) or 0

for i = 2, #ARGV, 2 do
    local field = ARGV[i]
    local delta = tonumber(ARGV[i + 1]) or 0
    if delta ~= 0 then
        redis.call("HINCRBY", KEYS[1], field, delta)
    end
end

if ttl > 0 then
    redis.call("EXPIRE", KEYS[1], ttl)
end

return 1
