local key = KEYS[1]
local field = ARGV[1]
local now_ts = tonumber(ARGV[2])
local ttl_seconds = tonumber(ARGV[3])

local function is_meta(name)
    return string.sub(name, 1, 1) == "_"
end

local function collect_ids()
    local ids = {}
    local keys = redis.call("HKEYS", key)
    for _, name in ipairs(keys) do
        if not is_meta(name) then
            table.insert(ids, tonumber(name))
        end
    end
    table.sort(ids)
    return ids
end

local ver = tonumber(redis.call("HGET", key, "_ver") or "0") + 1

if redis.call("HEXISTS", key, field) == 0 then
    redis.call("HSET", key, "_expire_at", tostring(now_ts + ttl_seconds), "_ver", tostring(ver))
    redis.call("EXPIRE", key, ttl_seconds)
    return 0
end

redis.call("HDEL", key, field)

local ids = collect_ids()
if #ids > 0 then
    redis.call("HSET", key, "_mincid", tostring(ids[1]))
else
    redis.call("HDEL", key, "_mincid")
end

redis.call("HSET", key, "_expire_at", tostring(now_ts + ttl_seconds), "_ver", tostring(ver))
redis.call("EXPIRE", key, ttl_seconds)

return 1
