package apitest

// cannedErrors answer an error status the schema declares without an example.
// They follow public_apis/errors.py + contract/errors.py ENGINE_CODES: every
// error carries error, code, type, retryable and request_id.
var cannedErrors = map[string]string{
	"400": `{"error":"media_type must be one of ai_video, ai_image_video, brainrot, stock, avatar.","code":"invalid_choice","field":"options.media_type","hint":"Pick a value from allowed_values.","type":"invalid_request","retryable":false,"request_id":"req_01HZX3K9QW"}`,
	"401": `{"error":"Invalid API key.","code":"invalid_api_key","hint":"Create a key at https://studio.zebracat.ai/api-keys.","type":"authentication","retryable":false,"request_id":"req_01HZX3K9QW"}`,
	"402": `{"error":"Your API balance is $0.00; this needs $4.80.","code":"insufficient_credits","hint":"Top up your API balance at https://studio.zebracat.ai/api-keys, then try again.","type":"insufficient_funds","retryable":false,"request_id":"req_01HZX3K9QW"}`,
	"403": `{"error":"This needs a paid plan.","code":"plan_locked","type":"permission","retryable":false,"request_id":"req_01HZX3K9QW"}`,
	"404": `{"error":"No video with this task_id.","code":"not_found","type":"not_found","retryable":false,"request_id":"req_01HZX3K9QW"}`,
	"409": `{"error":"The plan changed since rev 2.","code":"stale_rev","hint":"Re-read the plan and pass its rev as base_rev.","type":"conflict","retryable":false,"request_id":"req_01HZX3K9QW"}`,
	"422": `{"error":"The plan runs longer than this account allows.","code":"director_v2_too_long","type":"invalid_request","retryable":false,"request_id":"req_01HZX3K9QW"}`,
	"429": `{"error":"You already have 3 plans being written.","code":"too_many_runs","hint":"Wait for one to finish, or cancel one.","type":"rate_limited","retryable":true,"request_id":"req_01HZX3K9QW"}`,
	"502": `{"error":"The engine failed.","code":"generation_failed","type":"engine","retryable":true,"request_id":"req_01HZX3K9QW"}`,
}
