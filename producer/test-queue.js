const Redis = require('ioredis');

const redis = new Redis({
  host: 'localhost',
  port: 6379,
});

async function addJobs() {
  console.log('Adding 10 simple jobs to Redis list...');
  for (let i = 0; i < 10; i++) {
    const jobData = JSON.stringify({ jobId: `job-${i + 1}`, message: `hello world from simple job ${i + 1}` });
    await redis.rpush('simple-judge-queue', jobData);
    console.log(`Added simple job ${i + 1}: ${jobData}`);
  }
  console.log('Finished adding simple jobs.');
  redis.disconnect();
  process.exit(0);
}

addJobs().catch(err => {
  console.error('Error adding jobs:', err);
  process.exit(1);
});
