const Redis = require('ioredis');

const redis = new Redis({
  host: 'localhost',
  port: 6379,
});

async function addSubmissionJob(submissionId) {
  if (!submissionId) {
    console.error('Error: submissionId is required to add a job.');
    process.exit(1);
  }
  
  const jobPayload = JSON.stringify({ submissionId: submissionId });
  
  // Đẩy job vào đúng queue mà Judge Service đang lắng nghe
  await redis.rpush('submission_queue', jobPayload); 
  console.log(`Added submission job to 'submission_queue': ${jobPayload}`);
  
  redis.disconnect();
  process.exit(0);
}

// --- Hướng dẫn sử dụng ---
// THAY THẾ "YOUR_SUBMISSION_ID_HERE" bằng ID bài nộp THỰC TẾ của bạn từ MongoDB!
// Ví dụ: const MY_SUBMISSION_ID = "6863e14df71cc2e13c748a60"; 
const YOUR_ACTUAL_SUBMISSION_ID = "68654258ba087dc7941281a5"; 

addSubmissionJob(YOUR_ACTUAL_SUBMISSION_ID).catch(err => {
  console.error('Error adding submission job:', err);
  process.exit(1);
});