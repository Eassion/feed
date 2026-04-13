local userId = redis.call("GET", KEYS[1])
if not userId then
	return 0
end

if userId ~= ARGV[1] then
	return 0
end

local token = redis.call("GET", KEYS[2])
if not token then
	return 0
end

if token ~= ARGV[2] then
	return 0
end

local ttl = tonumber(ARGV[3])
if ttl and ttl > 0 then
	redis.call("EXPIRE", KEYS[1], ttl)
	redis.call("EXPIRE", KEYS[2], ttl)
end

return 1
