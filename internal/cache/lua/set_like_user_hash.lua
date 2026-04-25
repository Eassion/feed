local key = KEYS[1]
local field = ARGV[1]
local now_ts = tonumber(ARGV[2])
local ttl_seconds = tonumber(ARGV[3])
local capacity = tonumber(ARGV[4])
local content_id = tonumber(field)

local function is_meta(name)
    return string.sub(name, 1, 1) == "_"
end

local function collect_ids(exclude_field)
    local ids = {}
    local keys = redis.call("HKEYS", key)
    for _, name in ipairs(keys) do
        if not is_meta(name) and name ~= exclude_field then
            table.insert(ids, tonumber(name))
        end
    end
    table.sort(ids)
    return ids
end

local ver = tonumber(redis.call("HGET", key, "_ver") or "0") + 1

if redis.call("HEXISTS", key, field) == 1 then
    redis.call("HSET", key, "_expire_at", tostring(now_ts + ttl_seconds), "_ver", tostring(ver))
    redis.call("EXPIRE", key, ttl_seconds)
    return 0
end

local ids = collect_ids("")
local mincid = tonumber(redis.call("HGET", key, "_mincid") or "")
if mincid == nil and #ids > 0 then
    mincid = ids[1]
end

if capacity > 0 and #ids >= capacity and mincid ~= nil and content_id < mincid then
    redis.call("HSET", key, "_expire_at", tostring(now_ts + ttl_seconds), "_ver", tostring(ver))
    redis.call("EXPIRE", key, ttl_seconds)
    return 1
end

redis.call("HSET", key, field, "1")

if capacity > 0 and #ids >= capacity then
    local victim = mincid
    if victim ~= nil then
        redis.call("HDEL", key, tostring(victim))
    end
end

ids = collect_ids("")
if #ids > 0 then
    redis.call("HSET", key, "_mincid", tostring(ids[1]))
else
    redis.call("HDEL", key, "_mincid")
end

redis.call("HSET", key, "_expire_at", tostring(now_ts + ttl_seconds), "_ver", tostring(ver))
redis.call("EXPIRE", key, ttl_seconds)

return 1
