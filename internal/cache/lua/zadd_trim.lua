local keepN = tonumber(ARGV[1])

for i = 2, #ARGV, 2 do
	redis.call("ZADD", KEYS[1], ARGV[i], ARGV[i + 1])
end

if keepN and keepN > 0 then
	local size = redis.call("ZCARD", KEYS[1])
	if size > keepN then
		redis.call("ZREMRANGEBYRANK", KEYS[1], 0, size - keepN - 1)
	end
end

return redis.call("ZCARD", KEYS[1])
