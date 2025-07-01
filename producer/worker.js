const { Worker } = require('bullmq');

const worker = new Worker('submissions', async job => {
  console.log(`Processing job ${job.id} with message: ${job.data.message}`);
  // In a real scenario, this is where you'd trigger the Go Judge Daemon,
  // perhaps by making an HTTP call or putting it into another, simpler queue
  // that the Go daemon specifically listens to.

  // For now, we'll simulate sending it to the Go daemon by logging it.
  console.log(`Job ${job.id} processed by Node.js worker.`);

  // Optionally, return a result that the job producer can receive
  return { status: 'completed', message: `Processed: ${job.data.message}` };
}, {
  connection: {
    host: 'localhost',
    port: 6379,
  },
  // This ensures that even if the worker crashes, jobs are not lost
  // and are re-added to the queue after a timeout.
  autorun: true,
  concurrency: 1, // Process one job at a time
});

worker.on('completed', job => {
  console.log(`Job ${job.id} has completed!`);
});

worker.on('failed', (job, err) => {
  console.error(`Job ${job.id} has failed with error ${err.message}`);
});

console.log('Node.js BullMQ Worker started. Waiting for jobs...');
