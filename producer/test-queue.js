const { Queue } = require('bullmq');

const submissionQueue = new Queue('submissions', {
  connection: {
    host: 'localhost',
    port: 6379,
  },
});

async function addJobs() {
  console.log('Adding 10 jobs to the queue...');
  for (let i = 0; i < 10; i++) {
    await submissionQueue.add(
      'submissionJob',
      { message: `hello world from job ${i + 1}` },
      { jobId: `job-${i + 1}` }
    );
    console.log(`Added job ${i + 1}`);
  }
  console.log('Finished adding jobs.');
  process.exit(0);
}

addJobs().catch(err => {
  console.error('Error adding jobs:', err);
  process.exit(1);
});
