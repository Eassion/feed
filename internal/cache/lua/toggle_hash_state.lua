local current = redis.call("HGET", KEYS[1], ARGV[1])
local action = ARGV[2]

if action == "set" then
	if current == "1" then
		return 0
	end
	redis.call("HSET", KEYS[1], ARGV[1], "1")
	return 1
end

if action == "unset" then
	if not current then
		return 0
	end
	redis.call("HDEL", KEYS[1], ARGV[1])
	return 1
end

return redis.error_reply("unsupported action")
